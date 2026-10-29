package song

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const lyrics = `[Verse 1]
Korkein oot
korkein oot

Minun sieluni ylistää Sinua
rakkauden lähde

[Chorus]
Suuruutes äärellä viipyä kaipaan
`

func mustApply(t *testing.T, s *Song, op string, args any) {
	t.Helper()
	raw, _ := json.Marshal(args)
	if _, err := Apply(s, op, raw); err != nil {
		t.Fatalf("%s: %v", op, err)
	}
}

func TestParseLyrics(t *testing.T) {
	groups, err := ParseLyrics(lyrics)
	if err != nil {
		t.Fatal(err)
	}
	want := []Group{
		{Name: "Verse 1", Color: "#0077CC", Slides: []string{"Korkein oot\nkorkein oot", "Minun sieluni ylistää Sinua\nrakkauden lähde"}},
		{Name: "Chorus", Color: "#CC004E", Slides: []string{"Suuruutes äärellä viipyä kaipaan"}},
	}
	if !reflect.DeepEqual(groups, want) {
		t.Errorf("got %#v", groups)
	}
	if FormatLyrics(groups) != lyrics {
		t.Errorf("FormatLyrics does not reproduce the input:\n%s", FormatLyrics(groups))
	}
}

func TestParseLyricsRejects(t *testing.T) {
	for name, text := range map[string]string{
		"repeated group": "[Chorus]\na\n\n[Verse 1]\nb\n\n[Chorus]\na\n",
		"text first":     "hello\n[Verse 1]\na\n",
		"blank group":    "[Blank]\na\n",
		"no groups":      "\n\n",
	} {
		if _, err := ParseLyrics(text); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestLyricsReplaceCreatesDefaultArrangementOnce(t *testing.T) {
	s := New("Song", "Artist")
	mustApply(t, &s, "lyrics.replace", map[string]string{"text": lyrics})
	if s.SelectedArrangement != "Standard" || !reflect.DeepEqual(s.Arrangements[0].Groups, []string{"Blank", "Verse 1", "Chorus"}) {
		t.Fatalf("default arrangement: %+v selected %q", s.Arrangements, s.SelectedArrangement)
	}
	mustApply(t, &s, "arrangement.set", map[string]any{"name": "Standard", "groups": []string{"Blank", "Chorus", "Verse 1", "Chorus"}})
	mustApply(t, &s, "group.color", map[string]string{"name": "Chorus", "color": "aa0000"})
	mustApply(t, &s, "lyrics.replace", map[string]string{"text": lyrics + "\n[Bridge]\nPyhä\n"})
	if got := s.Arrangements[0].Groups; !reflect.DeepEqual(got, []string{"Blank", "Chorus", "Verse 1", "Chorus"}) {
		t.Errorf("lyrics.replace changed the arrangement: %v", got)
	}
	if s.Groups[1].Color != "#AA0000" {
		t.Errorf("lyrics.replace lost the chorus color: %s", s.Groups[1].Color)
	}
}

func TestRenameAndRemoveFollowArrangements(t *testing.T) {
	s := New("Song", "")
	mustApply(t, &s, "lyrics.replace", map[string]string{"text": lyrics})
	mustApply(t, &s, "arrangement.set", map[string]any{"name": "Short", "groups": []string{"Blank", "Chorus", "Verse 1", "Chorus"}})
	mustApply(t, &s, "group.rename", map[string]string{"name": "Chorus", "new_name": "Kertosäe"})
	if got := s.Arrangements[1].Groups; !reflect.DeepEqual(got, []string{"Blank", "Kertosäe", "Verse 1", "Kertosäe"}) {
		t.Errorf("after rename: %v", got)
	}
	mustApply(t, &s, "group.remove", map[string]string{"name": "Kertosäe"})
	if got := s.Arrangements[1].Groups; !reflect.DeepEqual(got, []string{"Blank", "Verse 1"}) {
		t.Errorf("after remove: %v", got)
	}
}

func TestFailedOpLeavesSongUnchanged(t *testing.T) {
	s := New("Song", "")
	mustApply(t, &s, "lyrics.replace", map[string]string{"text": lyrics})
	before, _ := json.Marshal(s)
	cases := []struct {
		op   string
		args string
	}{
		{"slide.set", `{"group":"Verse 1","index":3,"text":"x"}`},
		{"slide.add", `{"group":"Verse 1","text":"   \n  "}`},
		{"group.rename", `{"name":"Verse 1","new_name":"Chorus"}`},
		{"group.add", `{"name":"blank"}`},
		{"arrangement.set", `{"name":"X","groups":["Blank","Nope"]}`},
		{"slide.move", `{"group":"Verse 1","index":1,"to_group":"Chorus","to_index":9}`},
		{"group.color", `{"name":"Chorus","colour":"#fff"}`},
		{"nope", `{}`},
	}
	for _, c := range cases {
		if _, err := Apply(&s, c.op, json.RawMessage(c.args)); err == nil {
			t.Errorf("%s %s: want error", c.op, c.args)
		}
		if after, _ := json.Marshal(s); string(after) != string(before) {
			t.Fatalf("%s changed the song on error", c.op)
		}
	}
}

func TestSlideMoveAcrossGroups(t *testing.T) {
	s := New("Song", "")
	mustApply(t, &s, "lyrics.replace", map[string]string{"text": lyrics})
	mustApply(t, &s, "slide.move", map[string]any{"group": "Verse 1", "index": 2, "to_group": "Chorus", "to_index": 1})
	if len(s.Groups[0].Slides) != 1 || !strings.HasPrefix(s.Groups[1].Slides[0], "Minun sieluni") {
		t.Errorf("groups after move: %+v", s.Groups)
	}
}

func TestSlideTextIsCleaned(t *testing.T) {
	s := New("Song", "")
	mustApply(t, &s, "group.add", map[string]any{"name": "Tag", "slides": []string{"  first  \n\n second \n"}})
	if got := s.Groups[0].Slides[0]; got != "first\nsecond" {
		t.Errorf("got %q", got)
	}
}

func TestValidateHouseStyle(t *testing.T) {
	s := New("Song", "Artist")
	mustApply(t, &s, "group.add", map[string]any{"name": "Verse 1", "slides": []string{"one\ntwo\nthree", strings.Repeat("x", MaxLineChars+1)}})
	mustApply(t, &s, "arrangement.set", map[string]any{"name": "A", "groups": []string{"Verse 1"}})
	var got []string
	for _, i := range s.Validate() {
		got = append(got, i.Level+" "+i.Where+" "+i.Message)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"Verse 1 slide 1 3 lines", "Verse 1 slide 2 line has 37 characters", `arrangement A does not start with "Blank"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if HasErrors(s.Validate()) {
		t.Errorf("house style problems must be warnings, got:\n%s", joined)
	}
}
