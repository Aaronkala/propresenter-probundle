package song

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Issue struct {
	Level   string `json:"level"` // "error" blocks export; "warning" is house style
	Where   string `json:"where"`
	Message string `json:"message"`
}

// MaxLineChars is an estimate: 60 pt Helvetica Bold in capitals fills the 1620 pt text box at about 37 characters.
const MaxLineChars = 36

// MaxSlideLines is the house style: one or two lines per slide.
const MaxSlideLines = 2

func (s *Song) Validate() []Issue {
	var out []Issue
	add := func(level, where, format string, args ...any) {
		out = append(out, Issue{level, where, fmt.Sprintf(format, args...)})
	}
	if strings.TrimSpace(s.Title) == "" {
		add("error", "song", "title is empty")
	}
	if strings.ContainsAny(s.Title, "/:") {
		add("warning", "song", "title contains / or :, the file name uses - instead")
	}
	if strings.TrimSpace(s.Artist) == "" {
		add("warning", "song", "artist is empty")
	}
	if len(s.Groups) == 0 {
		add("error", "song", "no groups")
	}
	names := map[string]bool{}
	used := map[string]bool{}
	for _, a := range s.Arrangements {
		for _, n := range a.Groups {
			used[n] = true
		}
	}
	for _, g := range s.Groups {
		where := "group " + g.Name
		if names[g.Name] {
			add("error", where, "duplicate group name")
		}
		names[g.Name] = true
		if strings.EqualFold(g.Name, BlankGroup) {
			add("error", where, "name is reserved for the built-in first group")
		}
		if _, err := NormalizeColor(g.Color); err != nil {
			add("error", where, "%v", err)
		}
		if len(g.Slides) == 0 {
			add("error", where, "no slides")
		}
		if !used[g.Name] && len(s.Arrangements) > 0 {
			add("warning", where, "not used in any arrangement")
		}
		for i, t := range g.Slides {
			sw := fmt.Sprintf("%s slide %d", g.Name, i+1)
			if strings.TrimSpace(t) == "" {
				add("error", sw, "empty slide")
				continue
			}
			lines := strings.Split(t, "\n")
			if len(lines) > MaxSlideLines {
				add("warning", sw, "%d lines; house style is at most %d", len(lines), MaxSlideLines)
			}
			for _, l := range lines {
				if n := utf8.RuneCountInString(l); n > MaxLineChars {
					add("warning", sw, "line has %d characters and can wrap: %q", n, l)
				}
			}
		}
	}
	if len(s.Arrangements) == 0 {
		add("warning", "song", "no arrangements; ProPresenter plays groups in editor order")
	}
	arrNames := map[string]bool{}
	for _, a := range s.Arrangements {
		where := "arrangement " + a.Name
		if arrNames[a.Name] {
			add("error", where, "duplicate arrangement name")
		}
		arrNames[a.Name] = true
		if len(a.Groups) == 0 {
			add("error", where, "empty")
			continue
		}
		if a.Groups[0] != BlankGroup {
			add("warning", where, "does not start with %q", BlankGroup)
		}
		for _, n := range a.Groups {
			if n != BlankGroup && !names[n] {
				add("error", where, "unknown group %q", n)
			}
		}
	}
	if s.SelectedArrangement != "" && !arrNames[s.SelectedArrangement] {
		add("error", "song", "selected arrangement %q does not exist", s.SelectedArrangement)
	}
	if s.SelectedArrangement == "" && len(s.Arrangements) > 0 {
		add("warning", "song", "no selected arrangement")
	}
	return out
}

func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Level == "error" {
			return true
		}
	}
	return false
}
