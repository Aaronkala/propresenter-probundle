package song

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// OpSpec is one named change. The CLI and the UI both call ops by name, so they behave the same.
// Slide and position numbers are 1-based.
type OpSpec struct {
	Name string
	Args string // JSON shape, for the guide
	Doc  string
	run  func(*Song, json.RawMessage) (string, error)
}

func op[A any](name, args, doc string, f func(*Song, A) (string, error)) OpSpec {
	return OpSpec{Name: name, Args: args, Doc: doc, run: func(s *Song, raw json.RawMessage) (string, error) {
		var a A
		if len(bytes.TrimSpace(raw)) > 0 {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&a); err != nil {
				return "", fmt.Errorf("%s: bad arguments: %w (expected %s)", name, err, args)
			}
		}
		return f(s, a)
	}}
}

// Apply runs op name on s. On error s is unchanged.
func Apply(s *Song, name string, args json.RawMessage) (string, error) {
	for _, o := range Ops {
		if o.Name == name {
			work := s.clone()
			msg, err := o.run(&work, args)
			if err != nil {
				return "", err
			}
			*s = work
			return msg, nil
		}
	}
	return "", fmt.Errorf("unknown op %q", name)
}

func (s *Song) clone() Song {
	c := *s
	c.Groups = make([]Group, len(s.Groups))
	for i, g := range s.Groups {
		g.Slides = slices.Clone(g.Slides)
		c.Groups[i] = g
	}
	c.Arrangements = make([]Arrangement, len(s.Arrangements))
	for i, a := range s.Arrangements {
		a.Groups = slices.Clone(a.Groups)
		c.Arrangements[i] = a
	}
	return c
}

// CleanSlideText trims each line and drops empty lines. A slide cannot hold blank lines.
func CleanSlideText(text string) (string, error) {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("slide text is empty")
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Song) group(name string) (*Group, error) {
	if i := s.GroupIndex(name); i >= 0 {
		return &s.Groups[i], nil
	}
	if name == BlankGroup {
		return nil, fmt.Errorf("%q is the built-in first group and cannot be edited", BlankGroup)
	}
	return nil, fmt.Errorf("no group %q", name)
}

func (s *Song) arrangement(name string) (*Arrangement, error) {
	if i := s.ArrangementIndex(name); i >= 0 {
		return &s.Arrangements[i], nil
	}
	return nil, fmt.Errorf("no arrangement %q", name)
}

func checkGroupName(s *Song, name string) error {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return fmt.Errorf("group name is empty")
	case strings.EqualFold(name, BlankGroup):
		return fmt.Errorf("%q is reserved for the built-in first group", BlankGroup)
	case s.GroupIndex(name) >= 0:
		return fmt.Errorf("group %q already exists", name)
	}
	return nil
}

// insertAt puts v at 1-based position pos; pos 0 means the end.
func insertAt[T any](list []T, pos int, v T) ([]T, error) {
	if pos == 0 {
		return append(list, v), nil
	}
	if pos < 1 || pos > len(list)+1 {
		return nil, fmt.Errorf("position %d is out of range 1..%d", pos, len(list)+1)
	}
	return slices.Insert(list, pos-1, v), nil
}

func checkIndex(n, i int, what string) error {
	if i < 1 || i > n {
		return fmt.Errorf("%s %d is out of range 1..%d", what, i, n)
	}
	return nil
}

var Ops = []OpSpec{
	op("song.set", `{"field":"title|artist","value":"..."}`, "Set the song title or artist (CCLI credits).",
		func(s *Song, a struct{ Field, Value string }) (string, error) {
			v := strings.TrimSpace(a.Value)
			switch a.Field {
			case "title":
				if v == "" {
					return "", fmt.Errorf("title is empty")
				}
				s.Title = v
			case "artist":
				s.Artist = v
			default:
				return "", fmt.Errorf("field must be title or artist, not %q", a.Field)
			}
			return fmt.Sprintf("Set %s to %q", a.Field, v), nil
		}),
	op("song.replace", `{"song":{...same shape as show --json...}}`, "Replace the whole song. The song UUID is kept.",
		func(s *Song, a struct{ Song Song }) (string, error) {
			uuid := s.UUID
			*s = a.Song
			s.UUID = uuid
			for i := range s.Groups {
				if s.Groups[i].Color == "" {
					s.Groups[i].Color = DefaultColor(s.Groups[i].Name)
				}
			}
			return "Replaced the whole song", nil
		}),
	op("lyrics.replace", `{"text":"[Verse 1]\nline\nline\n\n..."}`,
		"Replace all groups and slides from lyrics text. Keeps colors of groups that keep their name. Creates arrangement \"Standard\" if none exists.",
		func(s *Song, a struct{ Text string }) (string, error) {
			groups, err := ParseLyrics(a.Text)
			if err != nil {
				return "", err
			}
			for i, g := range groups {
				if old := s.GroupIndex(g.Name); old >= 0 {
					groups[i].Color = s.Groups[old].Color
				}
			}
			s.Groups = groups
			if len(s.Arrangements) == 0 {
				s.Arrangements = []Arrangement{s.DefaultArrangement("Standard")}
				s.SelectedArrangement = "Standard"
			}
			return fmt.Sprintf("Replaced lyrics: %d groups", len(groups)), nil
		}),
	op("group.add", `{"name":"Verse 3","color":"#RRGGBB (optional)","slides":["..."],"index":0}`,
		"Add a group. index 0 = at the end.",
		func(s *Song, a struct {
			Name   string
			Color  string
			Slides []string
			Index  int
		},
		) (string, error) {
			name := strings.TrimSpace(a.Name)
			if err := checkGroupName(s, name); err != nil {
				return "", err
			}
			color := DefaultColor(name)
			if a.Color != "" {
				c, err := NormalizeColor(a.Color)
				if err != nil {
					return "", err
				}
				color = c
			}
			g := Group{Name: name, Color: color, Slides: []string{}}
			for _, t := range a.Slides {
				clean, err := CleanSlideText(t)
				if err != nil {
					return "", err
				}
				g.Slides = append(g.Slides, clean)
			}
			groups, err := insertAt(s.Groups, a.Index, g)
			if err != nil {
				return "", err
			}
			s.Groups = groups
			return fmt.Sprintf("Added group %q", name), nil
		}),
	op("group.rename", `{"name":"Verse","new_name":"Verse 1"}`, "Rename a group. Arrangements follow the new name.",
		func(s *Song, a struct {
			Name    string
			NewName string `json:"new_name"`
		},
		) (string, error) {
			g, err := s.group(a.Name)
			if err != nil {
				return "", err
			}
			nn := strings.TrimSpace(a.NewName)
			if nn == a.Name {
				return "Group name unchanged", nil
			}
			if err := checkGroupName(s, nn); err != nil {
				return "", err
			}
			g.Name = nn
			for i := range s.Arrangements {
				for j, n := range s.Arrangements[i].Groups {
					if n == a.Name {
						s.Arrangements[i].Groups[j] = nn
					}
				}
			}
			return fmt.Sprintf("Renamed group %q to %q", a.Name, nn), nil
		}),
	op("group.remove", `{"name":"Tag"}`, "Remove a group and its uses in arrangements.",
		func(s *Song, a struct{ Name string }) (string, error) {
			if _, err := s.group(a.Name); err != nil {
				return "", err
			}
			s.Groups = slices.DeleteFunc(s.Groups, func(g Group) bool { return g.Name == a.Name })
			for i := range s.Arrangements {
				s.Arrangements[i].Groups = slices.DeleteFunc(s.Arrangements[i].Groups, func(n string) bool { return n == a.Name })
			}
			return fmt.Sprintf("Removed group %q", a.Name), nil
		}),
	op("group.color", `{"name":"Chorus","color":"#CC004E"}`, "Set a group color.",
		func(s *Song, a struct{ Name, Color string }) (string, error) {
			g, err := s.group(a.Name)
			if err != nil {
				return "", err
			}
			c, err := NormalizeColor(a.Color)
			if err != nil {
				return "", err
			}
			g.Color = c
			return fmt.Sprintf("Set color of %q to %s", a.Name, c), nil
		}),
	op("group.move", `{"name":"Bridge","index":3}`, "Move a group to a 1-based position in the editor order.",
		func(s *Song, a struct {
			Name  string
			Index int
		},
		) (string, error) {
			i := s.GroupIndex(a.Name)
			if i < 0 {
				return "", fmt.Errorf("no group %q", a.Name)
			}
			if err := checkIndex(len(s.Groups), a.Index, "position"); err != nil {
				return "", err
			}
			g := s.Groups[i]
			s.Groups = slices.Insert(slices.Delete(s.Groups, i, i+1), a.Index-1, g)
			return fmt.Sprintf("Moved group %q to position %d", a.Name, a.Index), nil
		}),
	op("slide.add", `{"group":"Chorus","text":"line 1\nline 2","index":0}`, "Add a slide to a group. index 0 = at the end.",
		func(s *Song, a struct {
			Group string
			Text  string
			Index int
		},
		) (string, error) {
			g, err := s.group(a.Group)
			if err != nil {
				return "", err
			}
			t, err := CleanSlideText(a.Text)
			if err != nil {
				return "", err
			}
			slides, err := insertAt(g.Slides, a.Index, t)
			if err != nil {
				return "", err
			}
			g.Slides = slides
			return fmt.Sprintf("Added slide to %q (%d slides)", a.Group, len(slides)), nil
		}),
	op("slide.set", `{"group":"Chorus","index":2,"text":"..."}`, "Replace the text of one slide.",
		func(s *Song, a struct {
			Group string
			Index int
			Text  string
		},
		) (string, error) {
			g, err := s.group(a.Group)
			if err != nil {
				return "", err
			}
			if err := checkIndex(len(g.Slides), a.Index, "slide"); err != nil {
				return "", err
			}
			t, err := CleanSlideText(a.Text)
			if err != nil {
				return "", err
			}
			g.Slides[a.Index-1] = t
			return fmt.Sprintf("Changed %s slide %d", a.Group, a.Index), nil
		}),
	op("slide.remove", `{"group":"Chorus","index":2}`, "Remove one slide.",
		func(s *Song, a struct {
			Group string
			Index int
		},
		) (string, error) {
			g, err := s.group(a.Group)
			if err != nil {
				return "", err
			}
			if err := checkIndex(len(g.Slides), a.Index, "slide"); err != nil {
				return "", err
			}
			g.Slides = slices.Delete(g.Slides, a.Index-1, a.Index)
			return fmt.Sprintf("Removed %s slide %d", a.Group, a.Index), nil
		}),
	op("slide.move", `{"group":"Verse 1","index":2,"to_group":"Verse 2 (optional)","to_index":1}`,
		"Move a slide inside its group or to another group. to_index 0 = at the end.",
		func(s *Song, a struct {
			Group   string
			Index   int
			ToGroup string `json:"to_group"`
			ToIndex int    `json:"to_index"`
		},
		) (string, error) {
			src, err := s.group(a.Group)
			if err != nil {
				return "", err
			}
			if err := checkIndex(len(src.Slides), a.Index, "slide"); err != nil {
				return "", err
			}
			dstName := a.ToGroup
			if dstName == "" {
				dstName = a.Group
			}
			dst, err := s.group(dstName)
			if err != nil {
				return "", err
			}
			text := src.Slides[a.Index-1]
			src.Slides = slices.Delete(src.Slides, a.Index-1, a.Index)
			slides, err := insertAt(dst.Slides, a.ToIndex, text)
			if err != nil {
				return "", err
			}
			dst.Slides = slides
			return fmt.Sprintf("Moved %s slide %d to %s", a.Group, a.Index, dstName), nil
		}),
	op("arrangement.set", `{"name":"Standard","groups":["Blank","Verse 1","Chorus"]}`,
		"Create or replace an arrangement. The first arrangement becomes the selected one.",
		func(s *Song, a struct {
			Name   string
			Groups []string
		},
		) (string, error) {
			name := strings.TrimSpace(a.Name)
			if name == "" {
				return "", fmt.Errorf("arrangement name is empty")
			}
			for _, n := range a.Groups {
				if n != BlankGroup && s.GroupIndex(n) < 0 {
					return "", fmt.Errorf("arrangement %q uses unknown group %q", name, n)
				}
			}
			arr := Arrangement{Name: name, Groups: slices.Clone(a.Groups)}
			if arr.Groups == nil {
				arr.Groups = []string{}
			}
			if i := s.ArrangementIndex(name); i >= 0 {
				s.Arrangements[i] = arr
			} else {
				s.Arrangements = append(s.Arrangements, arr)
			}
			if s.SelectedArrangement == "" {
				s.SelectedArrangement = name
			}
			return fmt.Sprintf("Set arrangement %q (%d entries)", name, len(arr.Groups)), nil
		}),
	op("arrangement.rename", `{"name":"Standard","new_name":"Sunday"}`, "Rename an arrangement.",
		func(s *Song, a struct {
			Name    string
			NewName string `json:"new_name"`
		},
		) (string, error) {
			arr, err := s.arrangement(a.Name)
			if err != nil {
				return "", err
			}
			nn := strings.TrimSpace(a.NewName)
			if nn == "" {
				return "", fmt.Errorf("arrangement name is empty")
			}
			if nn != a.Name && s.ArrangementIndex(nn) >= 0 {
				return "", fmt.Errorf("arrangement %q already exists", nn)
			}
			arr.Name = nn
			if s.SelectedArrangement == a.Name {
				s.SelectedArrangement = nn
			}
			return fmt.Sprintf("Renamed arrangement %q to %q", a.Name, nn), nil
		}),
	op("arrangement.remove", `{"name":"Short"}`, "Remove an arrangement.",
		func(s *Song, a struct{ Name string }) (string, error) {
			if _, err := s.arrangement(a.Name); err != nil {
				return "", err
			}
			s.Arrangements = slices.DeleteFunc(s.Arrangements, func(x Arrangement) bool { return x.Name == a.Name })
			if s.SelectedArrangement == a.Name {
				s.SelectedArrangement = ""
				if len(s.Arrangements) > 0 {
					s.SelectedArrangement = s.Arrangements[0].Name
				}
			}
			return fmt.Sprintf("Removed arrangement %q", a.Name), nil
		}),
	op("arrangement.select", `{"name":"Standard"}`, "Select the arrangement ProPresenter uses by default.",
		func(s *Song, a struct{ Name string }) (string, error) {
			if _, err := s.arrangement(a.Name); err != nil {
				return "", err
			}
			s.SelectedArrangement = a.Name
			return fmt.Sprintf("Selected arrangement %q", a.Name), nil
		}),
}
