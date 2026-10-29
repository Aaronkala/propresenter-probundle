package rtf

import (
	"strings"
	"testing"
)

const testHeader = `{\rtf1\ansi\ansicpg1252\cocoartf2907
\cocoatextscaling0\cocoaplatform0{\fonttbl\f0\fnil\fcharset0 BeausiteClassic-Regular;}
{\colortbl;\red255\green255\blue255;\red255\green255\blue255;\red0\green0\blue0;}
{\*\expandedcolortbl;;\cssrgb\c100000\c100000\c100000;\cssrgb\c0\c0\c0;}
\deftab1300
\pard\tx0\pardeftab1300\pardirnatural\qc\partightenfactor0

\f0\b\fs120 \cf2 \outl0\strokewidth-40 \strokec3 `

func TestRoundTrip(t *testing.T) {
	for _, text := range []string{
		"Suuruutes äärellä vaipua saan\nSinun rakkautesi virtaan",
		"Jesus’ name — “holy” {braces} back\\slash",
		"Ω and emoji 🙏 after",
		"Å Ö ß é",
	} {
		got := Decode(Encode([]byte(testHeader), text))
		if got != text {
			t.Errorf("round trip of %q gave %q", text, got)
		}
	}
}

// The sample stores ä as \'e4 (CP1252), not as a Unicode escape.
func TestEncodeMatchesProPresenter(t *testing.T) {
	got := string(Encode([]byte(testHeader), "Käteni\nTiedän"))
	if want := testHeader + "K\\'e4teni\\\nTied\\'e4n}"; got != want {
		t.Errorf("got %q, want %q", got[len(testHeader):], want[len(testHeader):])
	}
}

func TestEncodeNormalizesToNFC(t *testing.T) {
	nfd := "a\u0308" // a + combining diaeresis, as macOS file names store it
	if got := string(Encode([]byte(testHeader), nfd)); !strings.HasSuffix(got, `\'e4}`) {
		t.Errorf("NFD input not composed: %q", got[len(testHeader):])
	}
}

func TestUTF16Len(t *testing.T) {
	cases := map[string]int{
		"Korkein oot\nkorkein oot": 23, // value from the sample
		"🙏":                        2,  // surrogate pair
		"a\u0308":                  1,  // composed first
	}
	for text, want := range cases {
		if got := UTF16Len(text); got != want {
			t.Errorf("UTF16Len(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestDecodeSkipsHeaderOfEmptySlide(t *testing.T) {
	// The Blank slide in the sample has a text element with this RTF and no text.
	empty := "{\\rtf1\\ansi\\ansicpg1252\\cocoartf2907\n\\cocoatextscaling0\\cocoaplatform0{\\fonttbl}\n{\\colortbl;\\red255\\green255\\blue255;}\n{\\*\\expandedcolortbl;;}\n}"
	if got := Decode([]byte(empty)); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestHeaderRequiresTextRunMarker(t *testing.T) {
	if _, err := Header([]byte(`{\rtf1 plain}`)); err == nil {
		t.Error("want error for RTF without \\strokec3")
	}
	h, err := Header([]byte(testHeader + "TEXT}"))
	if err != nil || string(h) != testHeader {
		t.Errorf("Header = %q, %v", h, err)
	}
}
