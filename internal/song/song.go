// Package song holds the editable song model that the CLI and the UI change.
// It knows nothing about protobuf; the bundle package converts it to a .pro file.
package song

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
)

// BlankGroup is the built-in first group: empty slide, "Worship slides look" macro and background video.
// It comes from the template and is not stored in Song.Groups.
const BlankGroup = "Blank"

type Song struct {
	UUID                string        `json:"uuid"`
	Title               string        `json:"title"`
	Artist              string        `json:"artist"`
	Groups              []Group       `json:"groups"`
	Arrangements        []Arrangement `json:"arrangements"`
	SelectedArrangement string        `json:"selected_arrangement"`
}

type Group struct {
	Name   string   `json:"name"`
	Color  string   `json:"color"` // #RRGGBB
	Slides []string `json:"slides"`
}

type Arrangement struct {
	Name   string   `json:"name"`
	Groups []string `json:"groups"` // group names; "Blank" allowed; repeats allowed
}

func New(title, artist string) Song {
	return Song{UUID: NewUUID(), Title: title, Artist: artist, Groups: []Group{}, Arrangements: []Arrangement{}}
}

// NewUUID returns a random version 4 UUID in uppercase, as ProPresenter writes them.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}

func (s *Song) GroupIndex(name string) int {
	for i, g := range s.Groups {
		if g.Name == name {
			return i
		}
	}
	return -1
}

func (s *Song) ArrangementIndex(name string) int {
	for i, a := range s.Arrangements {
		if a.Name == name {
			return i
		}
	}
	return -1
}

// DefaultArrangement plays Blank, then every group once in editor order.
func (s *Song) DefaultArrangement(name string) Arrangement {
	a := Arrangement{Name: name, Groups: []string{BlankGroup}}
	for _, g := range s.Groups {
		a.Groups = append(a.Groups, g.Name)
	}
	return a
}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func NormalizeColor(c string) (string, error) {
	c = strings.TrimSpace(c)
	if !strings.HasPrefix(c, "#") {
		c = "#" + c
	}
	if !hexColor.MatchString(c) {
		return "", fmt.Errorf("color %q is not #RRGGBB", c)
	}
	return strings.ToUpper(c), nil
}

// The first four colors are the group colors in the church sample; the others are this tool's picks.
var defaultColors = []struct{ prefix, color string }{
	{"verse 1", "#0077CC"},
	{"verse 2", "#005999"},
	{"verse 3", "#003B66"},
	{"verse", "#0077CC"},
	{"pre-chorus", "#E05A00"},
	{"prechorus", "#E05A00"},
	{"chorus", "#CC004E"},
	{"bridge", "#7600CC"},
	{"tag", "#B38600"},
	{"intro", "#5C5C5C"},
	{"outro", "#5C5C5C"},
	{"ending", "#5C5C5C"},
	{"interlude", "#5C5C5C"},
}

// DefaultColor picks a group color from the group name.
func DefaultColor(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, d := range defaultColors {
		if strings.HasPrefix(n, d.prefix) {
			return d.color
		}
	}
	return "#666666"
}
