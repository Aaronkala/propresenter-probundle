package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"google.golang.org/protobuf/proto"

	"probundle/internal/song"
)

func testSong(t *testing.T) song.Song {
	t.Helper()
	s := song.New("Suuruutes Äärellä", "Newland")
	s.Background = "Thermal Glow Electric - Medium - HD 1080"
	for _, op := range []struct {
		name string
		args any
	}{
		{"lyrics.replace", map[string]string{"text": "[Verse 1]\nKorkein oot\nkorkein oot\n\nMinun sieluni\n\n[Chorus]\nSuuruutes äärellä 🙏\n\n[Pre-Chorus]\nPyhä\n"}},
		{"arrangement.set", map[string]any{"name": "Standard", "groups": []string{"Blank", "Verse 1", "Pre-Chorus", "Chorus", "Verse 1", "Chorus", "Chorus"}}},
		{"arrangement.set", map[string]any{"name": "Short", "groups": []string{"Blank", "Chorus"}}},
		{"arrangement.select", map[string]string{"name": "Short"}},
	} {
		raw, _ := json.Marshal(op.args)
		if _, err := song.Apply(&s, op.name, raw); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// Export, read back, and import must give the same song.
func TestExportImportRoundTrip(t *testing.T) {
	s := testSong(t)
	out := filepath.Join(t.TempDir(), "song.probundle")
	if err := Export(s, out); err != nil {
		t.Fatal(err)
	}
	b, err := Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Notes) != 0 {
		t.Errorf("our own bundle needed repairs: %v", b.Notes)
	}
	if b.Manifest == nil || len(b.Manifest.Documents) != 1 || len(b.Manifest.Media) != 0 {
		t.Fatalf("manifest: %+v", b.Manifest)
	}
	doc := b.Manifest.Documents[0]
	if doc.OutputPath != FileTitle(s.Title)+".pro" || doc.Location.CorePath != "Songs/"+FileTitle(s.Title) {
		t.Errorf("manifest paths: %+v", doc)
	}
	p, _, err := b.Presentation()
	if err != nil {
		t.Fatal(err)
	}
	got, notes := ToSong(p, "")
	if len(notes) != 0 {
		t.Errorf("import notes: %v", notes)
	}
	if !reflect.DeepEqual(got, s) {
		g, _ := json.MarshalIndent(got, "", " ")
		w, _ := json.MarshalIndent(s, "", " ")
		t.Errorf("round trip differs\n got: %s\nwant: %s", g, w)
	}
}

func TestExportLinksLibraryGroupsAndBackground(t *testing.T) {
	s := testSong(t)
	p, err := BuildPresentation(s)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, _ := LoadTemplate()
	links := map[string]bool{}
	for _, cg := range p.GetCueGroups() {
		links[cg.GetGroup().GetName()] = cg.GetGroup().GetApplicationGroupIdentifier() != nil
	}
	if !links["Verse 1"] || !links["Chorus"] || links["Pre-Chorus"] {
		t.Errorf("library links: %v (library: %d groups)", links, len(tmpl.Library))
	}
	bg := MediaElement(p.GetCues()[0])
	if bg == nil || bg.GetUrl().GetLocal().GetPath() != "MotionBackgrounds2026/CMG - Thermal Glow - Motion Backgrounds/Thermal Glow Electric - Medium - HD 1080.mp4" {
		t.Errorf("background url: %v", bg.GetUrl())
	}
	if p.GetName() != s.Title || p.GetCcli().GetSongTitle() != s.Title {
		t.Errorf("name %q, ccli title %q", p.GetName(), p.GetCcli().GetSongTitle())
	}
}

// Exporting the same song twice gives the same .pro bytes, so re-exports do not create new objects.
func TestBuildIsDeterministic(t *testing.T) {
	s := testSong(t)
	a, _ := BuildPresentation(s)
	b, _ := BuildPresentation(s)
	ab, _ := proto.MarshalOptions{Deterministic: true}.Marshal(a)
	bb, _ := proto.MarshalOptions{Deterministic: true}.Marshal(b)
	if string(ab) != string(bb) {
		t.Error("two builds differ")
	}
}

func TestExportRefusesErrors(t *testing.T) {
	s := testSong(t)
	s.Background = "No such video"
	if err := Export(s, filepath.Join(t.TempDir(), "x.probundle")); err == nil {
		t.Error("want error for unknown background")
	}
}

// The first church sample has one group per slide; import merges them.
func TestImportMergesOneSlideGroups(t *testing.T) {
	path := "../../samples/Suuruutes Äärellä.probundle"
	if _, err := os.Stat(path); err != nil {
		t.Skip("sample not present")
	}
	b, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Notes) != 1 {
		t.Errorf("want the central directory repair note, got %v", b.Notes)
	}
	p, _, _ := b.Presentation()
	s, _ := ToSong(p, "")
	var names []string
	var counts []int
	for _, g := range s.Groups {
		names = append(names, g.Name)
		counts = append(counts, len(g.Slides))
	}
	if !reflect.DeepEqual(names, []string{"Verse 1", "Verse 2", "Chorus", "Bridge"}) || !reflect.DeepEqual(counts, []int{2, 2, 4, 1}) {
		t.Errorf("groups %v slides %v", names, counts)
	}
	want := []string{"Blank", "Verse 1", "Verse 2", "Chorus", "Verse 1", "Verse 2", "Chorus", "Chorus", "Bridge", "Bridge", "Bridge", "Bridge", "Chorus"}
	if got := s.Arrangements[0].Groups; !reflect.DeepEqual(got, want) {
		t.Errorf("arrangement %v", got)
	}
}
