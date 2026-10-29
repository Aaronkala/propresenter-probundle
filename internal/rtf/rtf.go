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

// headerEnd is the last control word before the text run in Cocoa RTF from ProPresenter.
const headerEnd = `\strokec3 `

// Header returns the part of a prototype slide RTF before the text. The header carries the
// font and colors, so new slides look like the prototype.
func Header(prototype []byte) ([]byte, error) {
	i := strings.LastIndex(string(prototype), headerEnd)
	if i < 0 {
		return nil, fmt.Errorf("RTF has no %q before the text", headerEnd)
	}
	return prototype[:i+len(headerEnd)], nil
}

// Encode returns the RTF bytes for plain slide text after header. "\n" is a line break.
func Encode(header []byte, text string) []byte {
	var b strings.Builder
	b.Write(header)
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
