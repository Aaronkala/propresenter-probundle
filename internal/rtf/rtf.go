// Package rtf writes and reads the Cocoa RTF that ProPresenter stores in lyric text elements.
package rtf

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/unicode/norm"
)

// header is copied byte for byte from the church sample (docs/probundle-format.md, section 4.5).
const header = `{\rtf1\ansi\ansicpg1252\cocoartf2907
\cocoatextscaling0\cocoaplatform0{\fonttbl\f0\fswiss\fcharset0 Helvetica-Bold;}
{\colortbl;\red255\green255\blue255;\red255\green255\blue255;\red0\green0\blue0;}
{\*\expandedcolortbl;;\cssrgb\c100000\c100000\c100000;\cssrgb\c0\c0\c0;}
\deftab1300
\pard\tx0\pardeftab1300\pardirnatural\qc\partightenfactor0

\f0\b\fs120 \cf2 \outl0\strokewidth-40 \strokec3 `

// Encode returns the RTF bytes for plain slide text. "\n" is a line break.
func Encode(text string) []byte {
	var b strings.Builder
	b.WriteString(header)
	for _, r := range norm.NFC.String(text) {
		switch {
		case r == '\n':
			b.WriteString("\\\n")
		case r == '\\' || r == '{' || r == '}':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\tab `)
		case r >= 0x20 && r < 0x7f:
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			// Control characters have no place on a slide.
		default:
			if c, ok := charmap.Windows1252.EncodeRune(r); ok {
				fmt.Fprintf(&b, `\'%02x`, c)
				continue
			}
			b.WriteString(`\uc0`)
			for _, u := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&b, `\u%d `, u)
			}
		}
	}
	b.WriteByte('}')
	return []byte(b.String())
}

// UTF16Len is the text length that ProPresenter stores in custom attribute ranges (NSRange units).
func UTF16Len(text string) int {
	return len(utf16.Encode([]rune(norm.NFC.String(text))))
}

// Decode extracts the plain text from ProPresenter lyric RTF. It handles the subset Cocoa writes for one text run.
func Decode(data []byte) string {
	s := string(data)
	if i := strings.LastIndex(s, `\strokec`); i >= 0 {
		s = s[i+len(`\strokec`):]
		s = strings.TrimLeft(s, "0123456789")
		s = strings.TrimPrefix(s, " ")
	}
	var out strings.Builder
	var units []uint16
	flushUnits := func() {
		if len(units) > 0 {
			out.WriteString(string(utf16.Decode(units)))
			units = units[:0]
		}
	}
	// The loop advances i by the length of each escape.
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '{' || c == '}' || c == '\r' || c == '\n' {
			continue
		}
		if c != '\\' || i+1 >= len(s) {
			flushUnits()
			out.WriteByte(c)
			continue
		}
		next := s[i+1]
		switch {
		case next == '\\' || next == '{' || next == '}':
			flushUnits()
			out.WriteByte(next)
			i++
		case next == '\n':
			flushUnits()
			out.WriteByte('\n')
			i++
		case next == '\'' && i+3 < len(s):
			flushUnits()
			v, _ := strconv.ParseUint(s[i+2:i+4], 16, 8)
			out.WriteRune(charmap.Windows1252.DecodeByte(byte(v)))
			i += 3
		default:
			// Control word: letters, optional signed number, optional space.
			j := i + 1
			for j < len(s) && (s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z') {
				j++
			}
			word := s[i+1 : j]
			k := j
			if k < len(s) && s[k] == '-' {
				k++
			}
			for k < len(s) && s[k] >= '0' && s[k] <= '9' {
				k++
			}
			num, _ := strconv.Atoi(s[j:k])
			if k < len(s) && s[k] == ' ' {
				k++
			}
			switch word {
			case "u":
				// RTF \u is a signed 16-bit value; uint16 keeps both forms right.
				units = append(units, uint16(num))
			case "tab":
				flushUnits()
				out.WriteByte('\t')
			case "line", "par":
				flushUnits()
				out.WriteByte('\n')
			}
			i = k - 1
		}
	}
	flushUnits()
	return out.String()
}
