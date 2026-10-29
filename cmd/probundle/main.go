// Command probundle builds ProPresenter .probundle song files. An LLM drives it with commands;
// the user reviews and edits in a browser UI and sends feedback back. See `probundle guide`.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"probundle/internal/bundle"
	"probundle/internal/server"
	"probundle/internal/song"
	"probundle/internal/store"
)

type command struct {
	name    string
	usage   string
	summary string
	run     func(args []string) error
}

var commands []command

func init() {
	commands = []command{
		{"guide", "", "Print the workflow and rules for an LLM", cmdGuide},
		{"new", "STATE --title T [--artist A] [--lyrics FILE|-] [--from BUNDLE] [--background NAME]", "Create a song state file", cmdNew},
		{"show", "STATE [--json | --lyrics]", "Print the song", cmdShow},
		{"lyrics", "STATE FILE|-", "Replace all groups and slides from lyrics text", cmdLyrics},
		{"set", "STATE title|artist|background VALUE", "Set a song field", cmdSet},
		{"group add", "STATE NAME [--color #RRGGBB] [--at N]", "Add a group", cmdGroupAdd},
		{"group rename", "STATE NAME NEW_NAME", "Rename a group (arrangements follow)", cmdOpArgs("group.rename", "name", "new_name")},
		{"group remove", "STATE NAME", "Remove a group and its arrangement entries", cmdOpArgs("group.remove", "name")},
		{"group color", "STATE NAME #RRGGBB", "Set a group color", cmdOpArgs("group.color", "name", "color")},
		{"group move", "STATE NAME POSITION", "Move a group in the editor order", cmdGroupMove},
		{"slide add", "STATE GROUP TEXT|- [--at N]", "Add a slide; \\n in TEXT is a line break", cmdSlideAdd},
		{"slide set", "STATE GROUP N TEXT|-", "Replace slide N of a group", cmdSlideSet},
		{"slide remove", "STATE GROUP N", "Remove slide N of a group", cmdSlideRemove},
		{"slide move", "STATE GROUP N TO [--to-group G]", "Move a slide", cmdSlideMove},
		{"arrangement set", "STATE NAME GROUP...", "Create or replace an arrangement", cmdArrangementSet},
		{"arrangement rename", "STATE NAME NEW_NAME", "Rename an arrangement", cmdOpArgs("arrangement.rename", "name", "new_name")},
		{"arrangement remove", "STATE NAME", "Remove an arrangement", cmdOpArgs("arrangement.remove", "name")},
		{"arrangement select", "STATE NAME", "Select the default arrangement", cmdOpArgs("arrangement.select", "name")},
		{"op", "STATE OP [JSON|-]", "Run any op by name (same ops as the UI); `probundle guide` lists them", cmdOp},
		{"validate", "STATE", "Print issues; exit 1 if there are errors", cmdValidate},
		{"ui", "STATE [--no-open] [--stop]", "Start (or reopen) the review UI in the browser", cmdUI},
		{"feedback", "STATE [--wait] [--timeout 10m] [--json]", "Print what the user did in the UI since the last call", cmdFeedback},
		{"export", "STATE [OUT.probundle]", "Write the .probundle", cmdExport},
		{"backgrounds", "", "List the background videos for the Blank slide", cmdBackgrounds},
		{"inspect", "BUNDLE [--json]", "Show what is inside a .probundle", cmdInspect},
		{"serve", "STATE [--addr 127.0.0.1:0]", "Run the UI server in the foreground (`ui` starts it for you)", cmdServe},
	}
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage(os.Stdout)
		return
	}
	for _, c := range commands {
		words := strings.Fields(c.name)
		if len(args) >= len(words) && strings.Join(args[:len(words)], " ") == c.name {
			if err := c.run(args[len(words):]); err != nil {
				var ex exitError
				if errors.As(err, &ex) {
					fmt.Fprintln(os.Stderr, ex.msg)
					os.Exit(ex.code)
				}
				fmt.Fprintf(os.Stderr, "probundle %s: %v\n", c.name, err)
				os.Exit(1)
			}
			return
		}
	}
	fmt.Fprintf(os.Stderr, "probundle: unknown command %q\n\n", strings.Join(args, " "))
	usage(os.Stderr)
	os.Exit(2)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "probundle: build ProPresenter .probundle song files. Start with `probundle guide`.")
	fmt.Fprintln(w)
	for _, c := range commands {
		fmt.Fprintf(w, "  probundle %s %s\n      %s\n", c.name, c.usage, c.summary)
	}
}

type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }

// parse accepts flags before, between and after positional arguments.
func parse(fs *flag.FlagSet, args []string, want int, usage string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) > 1 && strings.HasPrefix(a, "-") {
			name := strings.TrimLeft(a, "-")
			if j := strings.Index(name, "="); j >= 0 {
				name = name[:j]
			}
			f := fs.Lookup(name)
			if f == nil {
				return nil, fmt.Errorf("unknown flag %s (usage: %s)", a, usage)
			}
			flags = append(flags, a)
			if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); !strings.Contains(a, "=") && !(ok && bf.IsBoolFlag()) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		pos = append(pos, a)
	}
	if err := fs.Parse(flags); err != nil {
		return nil, fmt.Errorf("%v (usage: %s)", err, usage)
	}
	if want >= 0 && len(pos) != want {
		return nil, fmt.Errorf("need %d arguments, got %d (usage: %s)", want, len(pos), usage)
	}
	return pos, nil
}

func usageOf(name string) string {
	for _, c := range commands {
		if c.name == name {
			return "probundle " + c.name + " " + c.usage
		}
	}
	return name
}

// textArg reads "-" from stdin and turns a literal backslash-n into a line break.
func textArg(v string) (string, error) {
	if v == "-" {
		data, err := io.ReadAll(os.Stdin)
		return strings.TrimRight(string(data), "\n"), err
	}
	return strings.ReplaceAll(v, `\n`, "\n"), nil
}

func fileArg(v string) (string, error) {
	if v == "-" {
		data, err := io.ReadAll(os.Stdin)
		return string(data), err
	}
	data, err := os.ReadFile(v)
	return string(data), err
}

// apply runs one op on the state file, records it for the UI, and prints the result.
func apply(path, op string, args any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	var msg string
	st, err := store.Update(path, func(st *store.State) error {
		m, err := song.Apply(&st.Song, op, raw)
		if err != nil {
			return err
		}
		msg = m
		st.AddEvent("cli", "edit", m, nil)
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("rev %d: %s\n", st.Revision, msg)
	printIssues(bundle.Validate(st.Song))
	return nil
}

func printIssues(issues []song.Issue) {
	for _, i := range issues {
		fmt.Printf("  %s: %s: %s\n", i.Level, i.Where, i.Message)
	}
}

func cmdGuide([]string) error {
	fmt.Print(guide())
	return nil
}

func cmdNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	title := fs.String("title", "", "")
	artist := fs.String("artist", "", "")
	lyrics := fs.String("lyrics", "", "")
	from := fs.String("from", "", "")
	background := fs.String("background", "", "")
	pos, err := parse(fs, args, 1, usageOf("new"))
	if err != nil {
		return err
	}
	var s song.Song
	var notes []string
	if *from != "" {
		b, err := bundle.Read(*from)
		if err != nil {
			return err
		}
		p, name, err := b.Presentation()
		if err != nil {
			return err
		}
		s, notes = bundle.ToSong(p, strings.TrimSuffix(bundle.NormalizeName(name), ".pro"))
		if *title != "" {
			s.Title = *title
		}
		if *artist != "" {
			s.Artist = *artist
		}
	} else {
		if *title == "" {
			return fmt.Errorf("--title is required (usage: %s)", usageOf("new"))
		}
		s = song.New(*title, *artist)
	}
	if *background != "" {
		s.Background = *background
	}
	if *lyrics != "" {
		text, err := fileArg(*lyrics)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(map[string]string{"text": text})
		if _, err := song.Apply(&s, "lyrics.replace", raw); err != nil {
			return err
		}
	}
	st := &store.State{Song: s, Revision: 1}
	if err := store.Create(pos[0], st); err != nil {
		return err
	}
	fmt.Printf("created %s (rev 1): %q, %d groups\n", pos[0], s.Title, len(s.Groups))
	for _, n := range notes {
		fmt.Printf("  note: %s\n", n)
	}
	printIssues(bundle.Validate(s))
	return nil
}

func cmdShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	asLyrics := fs.Bool("lyrics", false, "")
	pos, err := parse(fs, args, 1, usageOf("show"))
	if err != nil {
		return err
	}
	st, err := store.Load(pos[0])
	if err != nil {
		return err
	}
	switch {
	case *asJSON:
		out, _ := json.MarshalIndent(st.Song, "", "  ")
		fmt.Println(string(out))
	case *asLyrics:
		fmt.Print(song.FormatLyrics(st.Song.Groups))
	default:
		printSong(st)
	}
	return nil
}

func printSong(st *store.State) {
	s := st.Song
	bg := s.Background
	if bg == "" {
		bg = "(default)"
	}
	fmt.Printf("%q by %q  (rev %d)\nbackground: %s\n", s.Title, s.Artist, st.Revision, bg)
	for _, g := range s.Groups {
		fmt.Printf("\n[%s] %s\n", g.Name, g.Color)
		for i, t := range g.Slides {
			fmt.Printf("  %d. %s\n", i+1, strings.ReplaceAll(t, "\n", " / "))
		}
	}
	fmt.Println("\narrangements:")
	for _, a := range s.Arrangements {
		mark := " "
		if a.Name == s.SelectedArrangement {
			mark = "*"
		}
		fmt.Printf("  %s %s: %s\n", mark, a.Name, strings.Join(a.Groups, " > "))
	}
	if issues := bundle.Validate(s); len(issues) > 0 {
		fmt.Println("\nissues:")
		printIssues(issues)
	}
}

func cmdLyrics(args []string) error {
	pos, err := parse(flag.NewFlagSet("lyrics", flag.ContinueOnError), args, 2, usageOf("lyrics"))
	if err != nil {
		return err
	}
	text, err := fileArg(pos[1])
	if err != nil {
		return err
	}
	return apply(pos[0], "lyrics.replace", map[string]string{"text": text})
}

func cmdSet(args []string) error {
	pos, err := parse(flag.NewFlagSet("set", flag.ContinueOnError), args, 3, usageOf("set"))
	if err != nil {
		return err
	}
	return apply(pos[0], "song.set", map[string]string{"field": pos[1], "value": pos[2]})
}

// cmdOpArgs maps positional arguments after STATE to op argument names.
func cmdOpArgs(op string, names ...string) func([]string) error {
	return func(args []string) error {
		pos, err := parse(flag.NewFlagSet(op, flag.ContinueOnError), args, len(names)+1, op)
		if err != nil {
			return err
		}
		m := map[string]string{}
		for i, n := range names {
			m[n] = pos[i+1]
		}
		return apply(pos[0], op, m)
	}
}

func cmdGroupAdd(args []string) error {
	fs := flag.NewFlagSet("group add", flag.ContinueOnError)
	color := fs.String("color", "", "")
	at := fs.Int("at", 0, "")
	pos, err := parse(fs, args, 2, usageOf("group add"))
	if err != nil {
		return err
	}
	return apply(pos[0], "group.add", map[string]any{"name": pos[1], "color": *color, "index": *at})
}

func atoi(s, what string) (int, error) {
	var n int
	if _, err := fmt.Sscan(s, &n); err != nil {
		return 0, fmt.Errorf("%s %q is not a number", what, s)
	}
	return n, nil
}

func cmdGroupMove(args []string) error {
	pos, err := parse(flag.NewFlagSet("group move", flag.ContinueOnError), args, 3, usageOf("group move"))
	if err != nil {
		return err
	}
	n, err := atoi(pos[2], "position")
	if err != nil {
		return err
	}
	return apply(pos[0], "group.move", map[string]any{"name": pos[1], "index": n})
}

func cmdSlideAdd(args []string) error {
	fs := flag.NewFlagSet("slide add", flag.ContinueOnError)
	at := fs.Int("at", 0, "")
	pos, err := parse(fs, args, 3, usageOf("slide add"))
	if err != nil {
		return err
	}
	text, err := textArg(pos[2])
	if err != nil {
		return err
	}
	return apply(pos[0], "slide.add", map[string]any{"group": pos[1], "text": text, "index": *at})
}

func cmdSlideSet(args []string) error {
	pos, err := parse(flag.NewFlagSet("slide set", flag.ContinueOnError), args, 4, usageOf("slide set"))
	if err != nil {
		return err
	}
	n, err := atoi(pos[2], "slide")
	if err != nil {
		return err
	}
	text, err := textArg(pos[3])
	if err != nil {
		return err
	}
	return apply(pos[0], "slide.set", map[string]any{"group": pos[1], "index": n, "text": text})
}

func cmdSlideRemove(args []string) error {
	pos, err := parse(flag.NewFlagSet("slide remove", flag.ContinueOnError), args, 3, usageOf("slide remove"))
	if err != nil {
		return err
	}
	n, err := atoi(pos[2], "slide")
	if err != nil {
		return err
	}
	return apply(pos[0], "slide.remove", map[string]any{"group": pos[1], "index": n})
}

func cmdSlideMove(args []string) error {
	fs := flag.NewFlagSet("slide move", flag.ContinueOnError)
	toGroup := fs.String("to-group", "", "")
	pos, err := parse(fs, args, 4, usageOf("slide move"))
	if err != nil {
		return err
	}
	n, err := atoi(pos[2], "slide")
	if err != nil {
		return err
	}
	to, err := atoi(pos[3], "target position")
	if err != nil {
		return err
	}
	return apply(pos[0], "slide.move", map[string]any{"group": pos[1], "index": n, "to_group": *toGroup, "to_index": to})
}

func cmdArrangementSet(args []string) error {
	pos, err := parse(flag.NewFlagSet("arrangement set", flag.ContinueOnError), args, -1, usageOf("arrangement set"))
	if err != nil {
		return err
	}
	if len(pos) < 3 {
		return fmt.Errorf("need STATE, NAME and at least one group (usage: %s)", usageOf("arrangement set"))
	}
	return apply(pos[0], "arrangement.set", map[string]any{"name": pos[1], "groups": pos[2:]})
}

func cmdOp(args []string) error {
	pos, err := parse(flag.NewFlagSet("op", flag.ContinueOnError), args, -1, usageOf("op"))
	if err != nil {
		return err
	}
	if len(pos) < 2 || len(pos) > 3 {
		return fmt.Errorf("usage: %s", usageOf("op"))
	}
	raw := "{}"
	if len(pos) == 3 {
		if raw, err = textArgRaw(pos[2]); err != nil {
			return err
		}
	}
	return apply(pos[0], pos[1], json.RawMessage(raw))
}

func textArgRaw(v string) (string, error) {
	if v == "-" {
		data, err := io.ReadAll(os.Stdin)
		return string(data), err
	}
	return v, nil
}

func cmdValidate(args []string) error {
	pos, err := parse(flag.NewFlagSet("validate", flag.ContinueOnError), args, 1, usageOf("validate"))
	if err != nil {
		return err
	}
	st, err := store.Load(pos[0])
	if err != nil {
		return err
	}
	issues := bundle.Validate(st.Song)
	if len(issues) == 0 {
		fmt.Println("no issues")
		return nil
	}
	printIssues(issues)
	if song.HasErrors(issues) {
		return exitError{1, "song has errors"}
	}
	return nil
}

func cmdUI(args []string) error {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	noOpen := fs.Bool("no-open", false, "")
	stop := fs.Bool("stop", false, "")
	pos, err := parse(fs, args, 1, usageOf("ui"))
	if err != nil {
		return err
	}
	if *stop {
		stopped, err := server.Stop(pos[0])
		if err != nil {
			return err
		}
		if stopped {
			fmt.Println("UI server stopped")
		} else {
			fmt.Println("UI server was not running")
		}
		return nil
	}
	url, started, err := server.Launch(pos[0], !*noOpen)
	if err != nil {
		return err
	}
	verb := "already running"
	if started {
		verb = "started"
	}
	fmt.Printf("UI %s: %s\n", verb, url)
	fmt.Printf("next: wait for the user with `probundle feedback %s --wait`\n", pos[0])
	return nil
}

// llmKinds are the UI events that mean the user wants the LLM to act.
var llmKinds = map[string]bool{"feedback": true, "approve": true, "export": true}

func cmdFeedback(args []string) error {
	fs := flag.NewFlagSet("feedback", flag.ContinueOnError)
	wait := fs.Bool("wait", false, "")
	timeout := fs.Duration("timeout", 10*time.Minute, "")
	asJSON := fs.Bool("json", false, "")
	pos, err := parse(fs, args, 1, usageOf("feedback"))
	if err != nil {
		return err
	}
	path := pos[0]
	deadline := time.Now().Add(*timeout)
	for {
		st, err := store.Load(path)
		if err != nil {
			return err
		}
		ready := !*wait
		for _, e := range st.Undelivered() {
			if e.Source == "ui" && llmKinds[e.Kind] {
				ready = true
			}
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			return exitError{2, fmt.Sprintf("no feedback from the user after %s; run the command again to keep waiting", *timeout)}
		}
		time.Sleep(500 * time.Millisecond)
	}
	var events []store.Event
	st, err := store.Update(path, func(st *store.State) error {
		for _, e := range st.Undelivered() {
			if e.Source == "ui" {
				events = append(events, e)
			}
		}
		if len(st.Events) == 0 || st.Delivered == st.Events[len(st.Events)-1].ID {
			return store.ErrNoChange
		}
		st.Delivered = st.Events[len(st.Events)-1].ID
		return nil
	})
	if err != nil {
		return err
	}
	if *asJSON {
		out, _ := json.MarshalIndent(map[string]any{"revision": st.Revision, "events": events}, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if len(events) == 0 {
		fmt.Println("no new events from the UI")
		return nil
	}
	fmt.Printf("%d new event(s) from the UI (song is now rev %d):\n", len(events), st.Revision)
	for _, e := range events {
		target := ""
		if e.Target != nil {
			target = " (" + e.Target.String() + ")"
		}
		fmt.Printf("- [%s]%s %s\n", e.Kind, target, e.Text)
	}
	fmt.Printf("run `probundle show %s` to see the current song\n", path)
	return nil
}

func cmdExport(args []string) error {
	pos, err := parse(flag.NewFlagSet("export", flag.ContinueOnError), args, -1, usageOf("export"))
	if err != nil {
		return err
	}
	if len(pos) < 1 || len(pos) > 2 {
		return fmt.Errorf("usage: %s", usageOf("export"))
	}
	st, err := store.Load(pos[0])
	if err != nil {
		return err
	}
	out := server.DefaultExportPath(pos[0], st)
	if len(pos) == 2 {
		out = pos[1]
	}
	if err := bundle.Export(st.Song, out); err != nil {
		return err
	}
	abs, _ := filepath.Abs(out)
	if _, err := store.Update(pos[0], func(st *store.State) error {
		st.ExportPath = abs
		st.AddEvent("cli", "export", "Exported "+abs, nil)
		return nil
	}); err != nil {
		return err
	}
	fmt.Printf("exported %s\n", abs)
	return nil
}

func cmdBackgrounds([]string) error {
	t, err := bundle.LoadTemplate()
	if err != nil {
		return err
	}
	for i, b := range t.Backgrounds {
		def := ""
		if i == 0 {
			def = "  (default)"
		}
		fmt.Printf("%s%s\n", b.GetName(), def)
	}
	return nil
}

func cmdInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	pos, err := parse(fs, args, 1, usageOf("inspect"))
	if err != nil {
		return err
	}
	b, err := bundle.Read(pos[0])
	if err != nil {
		return err
	}
	if *asJSON {
		p, _, err := b.Presentation()
		if err != nil {
			return err
		}
		out, err := protojson.MarshalOptions{Multiline: true, UseProtoNames: true}.Marshal(p)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	return bundle.Summary(os.Stdout, b)
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:0", "")
	pos, err := parse(fs, args, 1, usageOf("serve"))
	if err != nil {
		return err
	}
	return server.Run(pos[0], *addr)
}
