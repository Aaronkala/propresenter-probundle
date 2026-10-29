package song

import (
	"fmt"
	"strings"
)

// ParseLyrics reads the lyrics text format:
//
//	[Verse 1]
//	First line of slide 1
//	Second line of slide 1
//
//	First line of slide 2
//
//	[Chorus]
//	...
//
// A "[Name]" line starts a group. Blank lines separate slides. Lines in a slide become line breaks.
// Write each group once; repeats belong in arrangements.
func ParseLyrics(text string) ([]Group, error) {
	var groups []Group
	var slide []string
	flush := func() {
		if len(slide) > 0 {
			g := &groups[len(groups)-1]
			g.Slides = append(g.Slides, strings.Join(slide, "\n"))
			slide = nil
		}
	}
	seen := map[string]int{}
	for n, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"):
			if len(groups) > 0 {
				flush()
			}
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if name == "" {
				return nil, fmt.Errorf("line %d: empty group name", n+1)
			}
			if strings.EqualFold(name, BlankGroup) {
				return nil, fmt.Errorf("line %d: %q is the built-in first group; do not write it", n+1, BlankGroup)
			}
			if first, ok := seen[name]; ok {
				return nil, fmt.Errorf("line %d: group %q is already defined on line %d; write each group once and repeat it in an arrangement", n+1, name, first)
			}
			seen[name] = n + 1
			groups = append(groups, Group{Name: name, Color: DefaultColor(name), Slides: []string{}})
		case trimmed == "":
			if len(groups) > 0 {
				flush()
			}
		default:
			if len(groups) == 0 {
				return nil, fmt.Errorf("line %d: text before the first [Group] header", n+1)
			}
			slide = append(slide, trimmed)
		}
	}
	if len(groups) > 0 {
		flush()
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("no [Group] headers found")
	}
	return groups, nil
}

// FormatLyrics writes groups in the ParseLyrics format.
func FormatLyrics(groups []Group) string {
	var b strings.Builder
	for i, g := range groups {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%s]\n", g.Name)
		for j, s := range g.Slides {
			if j > 0 {
				b.WriteString("\n")
			}
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	return b.String()
}
