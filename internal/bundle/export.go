package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zeebo/blake3"
	"google.golang.org/protobuf/proto"

	"probundle/internal/rtf"
	"probundle/internal/rvpb"
	"probundle/internal/song"
)

// Library is the ProPresenter library the manifest points the song to.
const Library = "Songs"

// BuildPresentation turns a song into a .pro presentation, based on the embedded template.
func BuildPresentation(s song.Song) (*rvpb.Presentation, error) {
	t, err := LoadTemplate()
	if err != nil {
		return nil, err
	}
	id := func(key string) *rvpb.UUID { return &rvpb.UUID{String_: DerivedUUID(s.UUID + "/" + key)} }

	p := proto.Clone(t.Pres).(*rvpb.Presentation)
	p.Uuid = &rvpb.UUID{String_: strings.ToUpper(s.UUID)}
	p.Name = s.Title
	p.CueGroups, p.Cues, p.Arrangements = nil, nil, nil
	if p.Ccli == nil {
		p.Ccli = &rvpb.Presentation_CCLI{}
	}
	p.Ccli.SongTitle, p.Ccli.ArtistCredits, p.Ccli.Display = s.Title, s.Artist, true

	groupIDs := map[string]*rvpb.UUID{}

	// Blank group: copy the template cue with the chosen background, give every object its own UUID.
	background, err := t.Background(s.Background)
	if err != nil {
		return nil, err
	}
	blankGroup := proto.Clone(t.BlankGroup).(*rvpb.Presentation_CueGroup)
	blankCue := proto.Clone(t.BlankCue).(*rvpb.Cue)
	if err := applyBackground(blankCue, background); err != nil {
		return nil, err
	}
	reID(blankCue, "blank", id)
	blankGroup.Group.Uuid = id("group/blank")
	blankGroup.CueIdentifiers = []*rvpb.UUID{blankCue.Uuid}
	p.CueGroups = append(p.CueGroups, blankGroup)
	p.Cues = append(p.Cues, blankCue)
	groupIDs[song.BlankGroup] = blankGroup.Group.Uuid

	for gi, g := range s.Groups {
		key := "group/" + strconv.Itoa(gi) + "/" + g.Name
		cg := proto.Clone(t.LyricGroup).(*rvpb.Presentation_CueGroup)
		cg.Group.Uuid = id(key)
		cg.Group.Name = g.Name
		color, err := protoColor(g.Color)
		if err != nil {
			return nil, fmt.Errorf("group %q: %w", g.Name, err)
		}
		cg.Group.Color = color
		// Link to the church library group of the same name, so ProPresenter treats it as that group.
		cg.Group.ApplicationGroupIdentifier = nil
		if lib := t.LibraryGroup(g.Name); lib != nil {
			cg.Group.ApplicationGroupIdentifier = proto.Clone(lib.GetApplicationGroupIdentifier()).(*rvpb.UUID)
		}
		cg.CueIdentifiers = nil
		for si, text := range g.Slides {
			cue := proto.Clone(t.LyricCue).(*rvpb.Cue)
			reID(cue, key+"/slide/"+strconv.Itoa(si), id)
			setText(TextElement(cue), t.RTFHeader, text)
			cg.CueIdentifiers = append(cg.CueIdentifiers, cue.Uuid)
			p.Cues = append(p.Cues, cue)
		}
		p.CueGroups = append(p.CueGroups, cg)
		groupIDs[g.Name] = cg.Group.Uuid
	}

	for ai, a := range s.Arrangements {
		arr := &rvpb.Presentation_Arrangement{Uuid: id("arrangement/" + strconv.Itoa(ai) + "/" + a.Name), Name: a.Name}
		for _, n := range a.Groups {
			gid, ok := groupIDs[n]
			if !ok {
				return nil, fmt.Errorf("arrangement %q: unknown group %q", a.Name, n)
			}
			arr.GroupIdentifiers = append(arr.GroupIdentifiers, gid)
		}
		p.Arrangements = append(p.Arrangements, arr)
		if a.Name == s.SelectedArrangement {
			p.SelectedArrangement = arr.Uuid
		}
	}
	if s.SelectedArrangement == "" {
		p.SelectedArrangement = nil
	}
	return p, nil
}

// reID gives a cloned cue new UUIDs. It leaves references to objects outside the presentation alone:
// macro identification, library group link and media asset ID.
func reID(c *rvpb.Cue, key string, id func(string) *rvpb.UUID) {
	c.Uuid = id(key + "/cue")
	for i, a := range c.GetActions() {
		ak := key + "/action/" + strconv.Itoa(i)
		a.Uuid = id(ak)
		if bs := a.GetSlide().GetPresentation().GetBaseSlide(); bs != nil {
			bs.Uuid = id(ak + "/slide")
			for j, e := range bs.GetElements() {
				e.GetElement().Uuid = id(ak + "/element/" + strconv.Itoa(j))
			}
		}
		if m := a.GetMedia().GetElement(); m != nil {
			m.Uuid = id(ak + "/media")
		}
	}
}

func setText(e *rvpb.Graphics_Element, header []byte, text string) {
	e.Text.RtfData = rtf.Encode(header, text)
	n := int32(rtf.UTF16Len(text))
	for _, ca := range e.GetText().GetAttributes().GetCustomAttributes() {
		if ca.Range != nil {
			ca.Range.Start, ca.Range.End = 0, n
		}
	}
}

func protoColor(hexColor string) (*rvpb.Color, error) {
	c, err := song.NormalizeColor(hexColor)
	if err != nil {
		return nil, err
	}
	v, _ := strconv.ParseUint(c[1:], 16, 32)
	return &rvpb.Color{
		Red:   float32(v>>16&0xff) / 255,
		Green: float32(v>>8&0xff) / 255,
		Blue:  float32(v&0xff) / 255,
		Alpha: 1,
	}, nil
}

// Validate adds the template checks (background name) to the song checks.
func Validate(s song.Song) []song.Issue {
	issues := s.Validate()
	t, err := LoadTemplate()
	if err != nil {
		return append(issues, song.Issue{Level: "error", Where: "template", Message: err.Error()})
	}
	if _, err := t.Background(s.Background); err != nil {
		issues = append(issues, song.Issue{Level: "error", Where: "song", Message: err.Error()})
	}
	return issues
}

// Export writes the song as a .probundle to out. It refuses songs with validation errors.
func Export(s song.Song, out string) error {
	if issues := Validate(s); song.HasErrors(issues) {
		var msgs []string
		for _, i := range issues {
			if i.Level == "error" {
				msgs = append(msgs, i.Where+": "+i.Message)
			}
		}
		return fmt.Errorf("song has errors:\n  %s", strings.Join(msgs, "\n  "))
	}
	data, err := Encode(s, out)
	if err != nil {
		return err
	}
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}

// Encode builds the bundle bytes: manifest, then the .pro. No media is embedded; the Blank cue
// points to the background video by its path on the church Mac.
func Encode(s song.Song, out string) ([]byte, error) {
	start := time.Now()
	p, err := BuildPresentation(s)
	if err != nil {
		return nil, err
	}
	pro, err := proto.MarshalOptions{Deterministic: true}.Marshal(p)
	if err != nil {
		return nil, err
	}
	proName := FileTitle(s.Title) + ".pro"
	sum := blake3.Sum256(pro)
	root, _ := filepath.Abs(filepath.Dir(out))
	manifest := Manifest{
		Version:        1,
		TargetContract: "presentation_bundle",
		SourceRoot:     root,
		TargetRoot:     root,
		Documents: []ManifestDocument{{
			DocumentID: strings.ToLower(s.UUID),
			Location:   ManifestLocation{Type: "collection", Kind: "presentation", CorePath: Library + "/" + strings.TrimSuffix(proName, ".pro")},
			OutputPath: proName,
			ByteCount:  len(pro),
			Blake3:     hex.EncodeToString(sum[:]),
			Reopened:   true,
		}},
		Media:                 []ManifestMedia{},
		MediaReferences:       []ManifestMediaRef{},
		CompatibilityWarnings: []string{},
		ElapsedMillis:         int(time.Since(start).Milliseconds()),
	}
	mj, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, m := range []Member{{ManifestPath, mj}, {proName, pro}} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: m.Name, Method: zip.Store, Modified: start})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(m.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
