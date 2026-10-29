package bundle

import (
	_ "embed"
	"fmt"
	"net/url"
	"path"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"probundle/internal/rtf"
	"probundle/internal/rvpb"
)

// templatePro is made by cmd/make-template from the church samples. It is a presentation used as a container:
//
//   - the group whose cue has a media action is the Blank group (macro + background video);
//   - the group whose cue has a text element is the lyric prototype (one cue);
//   - groups without cues are the church library groups (name, color, application_group_identifier);
//   - cues that no group references are background choices (one media action each, cue name = background name).
//
//go:embed template.pro
var templatePro []byte

type Template struct {
	Pres        *rvpb.Presentation
	BlankGroup  *rvpb.Presentation_CueGroup
	BlankCue    *rvpb.Cue
	LyricGroup  *rvpb.Presentation_CueGroup
	LyricCue    *rvpb.Cue
	RTFHeader   []byte
	Library     []*rvpb.Group // known library groups
	Backgrounds []*rvpb.Cue   // background choices; Backgrounds[0] is the default
}

func LoadTemplate() (*Template, error) {
	p := &rvpb.Presentation{}
	if err := proto.Unmarshal(templatePro, p); err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	return ParseTemplate(p)
}

// ParseTemplate splits a template container (see templatePro) into its parts.
func ParseTemplate(p *rvpb.Presentation) (*Template, error) {
	t := &Template{Pres: p}
	cues := map[string]*rvpb.Cue{}
	for _, c := range p.GetCues() {
		cues[c.GetUuid().GetString_()] = c
	}
	referenced := map[string]bool{}
	for _, g := range p.GetCueGroups() {
		if len(g.GetCueIdentifiers()) == 0 {
			t.Library = append(t.Library, g.GetGroup())
			continue
		}
		for _, id := range g.GetCueIdentifiers() {
			referenced[id.GetString_()] = true
		}
		cue := cues[g.GetCueIdentifiers()[0].GetString_()]
		switch {
		case cue == nil:
		case t.BlankGroup == nil && MediaElement(cue) != nil:
			t.BlankGroup, t.BlankCue = g, cue
		case t.LyricGroup == nil && TextElement(cue) != nil:
			t.LyricGroup, t.LyricCue = g, cue
		}
	}
	for _, c := range p.GetCues() {
		if !referenced[c.GetUuid().GetString_()] && MediaElement(c) != nil {
			t.Backgrounds = append(t.Backgrounds, c)
		}
	}
	if t.BlankCue == nil || t.LyricCue == nil {
		return nil, fmt.Errorf("template needs a group with a media cue and a group with a text slide")
	}
	header, err := rtf.Header(TextElement(t.LyricCue).GetText().GetRtfData())
	if err != nil {
		return nil, fmt.Errorf("template lyric slide: %w", err)
	}
	t.RTFHeader = header
	return t, nil
}

// Background returns the background cue with this name; "" means the default.
func (t *Template) Background(name string) (*rvpb.Cue, error) {
	if len(t.Backgrounds) == 0 {
		return nil, fmt.Errorf("template has no backgrounds")
	}
	if name == "" {
		return t.Backgrounds[0], nil
	}
	for _, b := range t.Backgrounds {
		if b.GetName() == name {
			return b, nil
		}
	}
	return nil, fmt.Errorf("unknown background %q (see `probundle backgrounds`)", name)
}

// LibraryGroup returns the church library group with this name, or nil.
func (t *Template) LibraryGroup(name string) *rvpb.Group {
	for _, g := range t.Library {
		if g.GetName() == name {
			return g
		}
	}
	return nil
}

// TextElement returns the first text element of the cue's slide, or nil.
func TextElement(c *rvpb.Cue) *rvpb.Graphics_Element {
	for _, a := range c.GetActions() {
		for _, e := range a.GetSlide().GetPresentation().GetBaseSlide().GetElements() {
			if e.GetElement().GetText().GetRtfData() != nil {
				return e.GetElement()
			}
		}
	}
	return nil
}

// MediaElement returns the media element of the cue's first media action, or nil.
func MediaElement(c *rvpb.Cue) *rvpb.Media {
	for _, a := range c.GetActions() {
		if m := a.GetMedia().GetElement(); m != nil {
			return m
		}
	}
	return nil
}

// FileURL builds the URL form ProPresenter uses for a file outside the bundle:
// absolute file:// string, macOS platform, and a path relative to a known folder.
func FileURL(absPath string) *rvpb.URL {
	u := &rvpb.URL{
		Storage:  &rvpb.URL_AbsoluteString{AbsoluteString: (&url.URL{Scheme: "file", Path: absPath}).String()},
		Platform: rvpb.URL_PLATFORM_MACOS,
	}
	local := &rvpb.URL_LocalRelativePath{Root: rvpb.URL_LocalRelativePath_ROOT_BOOT_VOLUME, Path: strings.TrimPrefix(absPath, "/")}
	// /Users/<name>/Documents/... is relative to the documents folder of whoever runs ProPresenter.
	if parts := strings.SplitN(absPath, "/", 5); len(parts) == 5 && parts[1] == "Users" && parts[3] == "Documents" {
		local = &rvpb.URL_LocalRelativePath{Root: rvpb.URL_LocalRelativePath_ROOT_USER_DOCUMENTS, Path: parts[4]}
	}
	u.RelativeFilePath = &rvpb.URL_Local{Local: local}
	return u
}

// SetVideoURL points a video media element at u in all three places ProPresenter stores it.
// The third place is field 6 of VideoTypeProperties, which the public schema does not name:
// {1: FileProperties{local_url: URL}}.
func SetVideoURL(m *rvpb.Media, u *rvpb.URL) error {
	video := m.GetVideo()
	if video == nil {
		return fmt.Errorf("media element is not a video")
	}
	m.Url = proto.Clone(u).(*rvpb.URL)
	if video.File == nil {
		video.File = &rvpb.FileProperties{}
	}
	video.File.LocalUrl = proto.Clone(u).(*rvpb.URL)

	fp, err := proto.MarshalOptions{Deterministic: true}.Marshal(&rvpb.FileProperties{LocalUrl: u})
	if err != nil {
		return err
	}
	inner := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), fp)
	var kept []byte
	unknown := video.ProtoReflect().GetUnknown()
	for len(unknown) > 0 {
		num, typ, n := protowire.ConsumeTag(unknown)
		if n < 0 {
			return fmt.Errorf("bad unknown field data in video properties")
		}
		m := protowire.ConsumeFieldValue(num, typ, unknown[n:])
		if m < 0 {
			return fmt.Errorf("bad unknown field data in video properties")
		}
		if num != 6 {
			kept = append(kept, unknown[:n+m]...)
		}
		unknown = unknown[n+m:]
	}
	kept = protowire.AppendBytes(protowire.AppendTag(kept, 6, protowire.BytesType), inner)
	video.ProtoReflect().SetUnknown(kept)
	return nil
}

// MakeTemplate builds the template container. primary gives the Blank cue and the lyric prototype;
// every bundle adds its background and its library group links.
func MakeTemplate(primary *Bundle, others ...*Bundle) (*rvpb.Presentation, error) {
	src, _, err := primary.Presentation()
	if err != nil {
		return nil, err
	}
	t, err := ParseTemplate(proto.Clone(src).(*rvpb.Presentation))
	if err != nil {
		return nil, err
	}
	p := t.Pres
	p.Uuid = &rvpb.UUID{String_: "00000000-0000-0000-0000-000000000000"}
	p.Name = ""
	p.Arrangements, p.SelectedArrangement = nil, nil
	if p.Ccli != nil {
		p.Ccli.SongTitle, p.Ccli.ArtistCredits = "", ""
	}
	t.LyricGroup.CueIdentifiers = t.LyricGroup.CueIdentifiers[:1]
	setText(TextElement(t.LyricCue), t.RTFHeader, "Template")
	p.CueGroups = []*rvpb.Presentation_CueGroup{t.BlankGroup, t.LyricGroup}
	p.Cues = []*rvpb.Cue{t.BlankCue, t.LyricCue}

	seenGroup := map[string]bool{}
	seenBackground := map[string]bool{}
	for _, b := range append([]*Bundle{primary}, others...) {
		bp, _, err := b.Presentation()
		if err != nil {
			return nil, err
		}
		bt, err := ParseTemplate(bp)
		if err != nil {
			return nil, err
		}
		for _, cg := range bp.GetCueGroups() {
			g := cg.GetGroup()
			if g.GetName() == "" || g.GetName() == "Blank" || g.GetApplicationGroupIdentifier() == nil || seenGroup[g.GetName()] {
				continue
			}
			seenGroup[g.GetName()] = true
			lib := proto.Clone(g).(*rvpb.Group)
			lib.Uuid = nil
			p.CueGroups = append(p.CueGroups, &rvpb.Presentation_CueGroup{Group: lib})
		}
		bg, err := backgroundFrom(b, bt.BlankCue)
		if err != nil {
			return nil, err
		}
		if !seenBackground[bg.GetName()] {
			seenBackground[bg.GetName()] = true
			p.Cues = append(p.Cues, bg)
		}
	}
	// The Blank cue in the template uses the default background, outside the bundle.
	if err := applyBackground(t.BlankCue, p.Cues[2]); err != nil {
		return nil, err
	}
	return p, nil
}

// backgroundFrom extracts the media action of a Blank cue as a background choice,
// pointing at the original file path from the manifest instead of the bundle copy.
func backgroundFrom(b *Bundle, blank *rvpb.Cue) (*rvpb.Cue, error) {
	for i, a := range blank.GetActions() {
		m := a.GetMedia().GetElement()
		if m == nil {
			continue
		}
		source := ""
		if b.Manifest != nil {
			want := fmt.Sprintf("/root/cues/%s/actions/%d/", blank.GetUuid().GetString_(), i)
			for _, r := range b.Manifest.MediaReferences {
				if strings.HasPrefix(r.OccurrencePath, want) {
					source = r.SourcePath
				}
			}
		}
		if source == "" {
			return nil, fmt.Errorf("no manifest source path for the media in the Blank cue")
		}
		action := proto.Clone(a).(*rvpb.Action)
		if err := SetVideoURL(action.GetMedia().GetElement(), FileURL(source)); err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(path.Base(source), path.Ext(source))
		return &rvpb.Cue{Uuid: &rvpb.UUID{String_: strings.ToUpper(m.GetUuid().GetString_())}, Name: name, Actions: []*rvpb.Action{action}}, nil
	}
	return nil, fmt.Errorf("Blank cue has no media action")
}

// applyBackground puts the background's media action into the Blank cue and names the slide after it.
func applyBackground(blank, background *rvpb.Cue) error {
	media := background.GetActions()[0]
	for i, a := range blank.GetActions() {
		if a.GetMedia() != nil {
			uuid := a.Uuid
			blank.Actions[i] = proto.Clone(media).(*rvpb.Action)
			blank.Actions[i].Uuid = uuid
		}
		if a.GetSlide() != nil && a.GetLabel() != nil {
			a.Label.Text = background.GetName() + ".mp4"
		}
	}
	if MediaElement(blank) == nil {
		return fmt.Errorf("Blank cue has no media action")
	}
	return nil
}
