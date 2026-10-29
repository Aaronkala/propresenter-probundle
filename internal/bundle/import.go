package bundle

import (
	"fmt"
	"slices"
	"strings"

	"probundle/internal/rtf"
	"probundle/internal/rvpb"
	"probundle/internal/song"
)

// ToSong reads a presentation back into the song model. title is used when the CCLI title is empty.
// Consecutive groups with the same name become one group with several slides,
// which repairs the "one group per slide" layout of older songs.
func ToSong(p *rvpb.Presentation, title string) (song.Song, []string) {
	var notes []string
	s := song.Song{
		UUID:         strings.ToUpper(p.GetUuid().GetString_()),
		Title:        p.GetCcli().GetSongTitle(),
		Artist:       p.GetCcli().GetArtistCredits(),
		Groups:       []song.Group{},
		Arrangements: []song.Arrangement{},
	}
	if s.Title == "" {
		s.Title = title
	}
	if s.UUID == "" {
		s.UUID = song.NewUUID()
	}
	cues := map[string]*rvpb.Cue{}
	for _, c := range p.GetCues() {
		cues[c.GetUuid().GetString_()] = c
	}

	// parts maps each merged group name to the presentation group UUIDs it was made from, in order.
	parts := map[string][]string{}
	groupOf := map[string]string{}
	prevName := ""
	for _, cg := range p.GetCueGroups() {
		g := cg.GetGroup()
		gid := g.GetUuid().GetString_()
		name := g.GetName()
		var first *rvpb.Cue
		if ids := cg.GetCueIdentifiers(); len(ids) > 0 {
			first = cues[ids[0].GetString_()]
		}
		// The Blank group is the one that starts the background video. Older songs also name it "Blank".
		if name == song.BlankGroup || first != nil && MediaElement(first) != nil {
			groupOf[gid] = song.BlankGroup
			prevName = ""
			if s.Background == "" {
				s.Background, notes = backgroundName(first, notes)
			}
			continue
		}
		var slides []string
		for _, cid := range cg.GetCueIdentifiers() {
			if c := cues[cid.GetString_()]; c != nil {
				if e := TextElement(c); e != nil {
					slides = append(slides, rtf.Decode(e.GetText().GetRtfData()))
				}
			}
		}
		if name == prevName && len(s.Groups) > 0 {
			last := &s.Groups[len(s.Groups)-1]
			last.Slides = append(last.Slides, slides...)
			parts[last.Name] = append(parts[last.Name], gid)
			groupOf[gid] = last.Name
			continue
		}
		unique := name
		for n := 2; s.GroupIndex(unique) >= 0; n++ {
			unique = fmt.Sprintf("%s (%d)", name, n)
		}
		if unique != name {
			notes = append(notes, fmt.Sprintf("second group named %q renamed to %q", name, unique))
		}
		s.Groups = append(s.Groups, song.Group{Name: unique, Color: hexColor(g.GetColor()), Slides: slides})
		parts[unique] = []string{gid}
		groupOf[gid] = unique
		prevName = name
	}

	for _, a := range p.GetArrangements() {
		ids := make([]string, len(a.GetGroupIdentifiers()))
		for i, u := range a.GetGroupIdentifiers() {
			ids[i] = u.GetString_()
		}
		arr := song.Arrangement{Name: a.GetName(), Groups: []string{}}
		for i := 0; i < len(ids); {
			name, ok := groupOf[ids[i]]
			if !ok {
				notes = append(notes, fmt.Sprintf("arrangement %q: unknown group %s dropped", a.GetName(), ids[i]))
				i++
				continue
			}
			arr.Groups = append(arr.Groups, name)
			// A merged group plays all its parts; consume the full run of parts as one entry.
			if pp := parts[name]; len(pp) > 1 && i+len(pp) <= len(ids) && slices.Equal(ids[i:i+len(pp)], pp) {
				i += len(pp)
			} else {
				if len(pp) > 1 {
					notes = append(notes, fmt.Sprintf("arrangement %q: partial use of %q became the whole group", a.GetName(), name))
				}
				i++
			}
		}
		s.Arrangements = append(s.Arrangements, arr)
		if a.GetUuid().GetString_() == p.GetSelectedArrangement().GetString_() {
			s.SelectedArrangement = arr.Name
		}
	}
	return s, notes
}

func hexColor(c *rvpb.Color) string {
	to := func(f float32) int { return int(f*255 + 0.5) }
	return fmt.Sprintf("#%02X%02X%02X", to(c.GetRed()), to(c.GetGreen()), to(c.GetBlue()))
}

// backgroundName finds the template background used by a Blank cue. The slide action label holds the file name.
func backgroundName(blank *rvpb.Cue, notes []string) (string, []string) {
	if blank == nil {
		return "", notes
	}
	t, err := LoadTemplate()
	if err != nil {
		return "", append(notes, err.Error())
	}
	for _, a := range blank.GetActions() {
		label := strings.TrimSuffix(a.GetLabel().GetText(), ".mp4")
		if label == "" {
			continue
		}
		if _, err := t.Background(label); err == nil {
			return label, notes
		}
		return "", append(notes, fmt.Sprintf("background %q is not in the template; using the default", label))
	}
	return "", notes
}
