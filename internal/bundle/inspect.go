package bundle

import (
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/zeebo/blake3"

	"probundle/internal/rtf"
	"probundle/internal/rvpb"
)

// Summary prints the bundle layout, the manifest hash check and the presentation outline.
func Summary(w io.Writer, b *Bundle) error {
	fmt.Fprintln(w, "== members ==")
	for _, n := range b.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
	for _, m := range b.Members {
		form := ""
		if m.Name != NormalizeName(m.Name) {
			form = " (NFD name)"
		}
		fmt.Fprintf(w, "  %10d  %s%s\n", len(m.Data), m.Name, form)
	}
	if b.Manifest != nil {
		fmt.Fprintln(w, "\n== manifest check ==")
		check := func(path string, count int, sum string) {
			m := b.Member(path)
			status := "MISSING"
			if m != nil {
				got := blake3.Sum256(m.Data)
				status = "BAD"
				if len(m.Data) == count && hex.EncodeToString(got[:]) == sum {
					status = "OK"
				}
			}
			fmt.Fprintf(w, "  %-7s %s\n", status, path)
		}
		for _, d := range b.Manifest.Documents {
			check(d.OutputPath, d.ByteCount, d.Blake3)
		}
		for _, m := range b.Manifest.Media {
			check(m.OutputPath, m.ByteCount, m.Blake3)
		}
	}
	p, name, err := b.Presentation()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\n== %s ==\n", name)
	PrintPresentation(w, p)
	return nil
}

func PrintPresentation(w io.Writer, p *rvpb.Presentation) {
	groups := map[string]string{}
	for _, g := range p.GetCueGroups() {
		groups[g.GetGroup().GetUuid().GetString_()] = g.GetGroup().GetName()
	}
	cues := map[string]*rvpb.Cue{}
	for _, c := range p.GetCues() {
		cues[c.GetUuid().GetString_()] = c
	}
	fmt.Fprintf(w, "uuid: %s\n", p.GetUuid().GetString_())
	fmt.Fprintf(w, "ccli: title=%q artist=%q\n", p.GetCcli().GetSongTitle(), p.GetCcli().GetArtistCredits())
	for _, a := range p.GetArrangements() {
		sel := ""
		if a.GetUuid().GetString_() == p.GetSelectedArrangement().GetString_() {
			sel = " (selected)"
		}
		var names []string
		for _, g := range a.GetGroupIdentifiers() {
			names = append(names, groups[g.GetString_()])
		}
		fmt.Fprintf(w, "arrangement %q%s: %s\n", a.GetName(), sel, strings.Join(names, " > "))
	}
	for _, cg := range p.GetCueGroups() {
		g := cg.GetGroup()
		fmt.Fprintf(w, "\n[%s] %s %s\n", g.GetName(), hexColor(g.GetColor()), g.GetUuid().GetString_())
		for i, cid := range cg.GetCueIdentifiers() {
			c := cues[cid.GetString_()]
			if c == nil {
				fmt.Fprintf(w, "  %d. MISSING CUE %s\n", i+1, cid.GetString_())
				continue
			}
			var kinds []string
			for _, a := range c.GetActions() {
				kinds = append(kinds, strings.TrimPrefix(a.GetType().String(), "ACTION_TYPE_"))
			}
			fmt.Fprintf(w, "  %d. actions: %s\n", i+1, strings.Join(kinds, ", "))
			if e := TextElement(c); e != nil {
				for _, l := range strings.Split(rtf.Decode(e.GetText().GetRtfData()), "\n") {
					fmt.Fprintf(w, "     | %s\n", l)
				}
			}
		}
	}
}
