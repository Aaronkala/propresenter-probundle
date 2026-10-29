package main

import (
	"fmt"
	"strings"

	"probundle/internal/song"
)

func guide() string {
	var ops strings.Builder
	for _, o := range song.Ops {
		fmt.Fprintf(&ops, "  %-20s %s\n  %-20s args: %s\n", o.Name, o.Doc, "", o.Args)
	}
	return fmt.Sprintf(`probundle guide (for LLMs)

probundle keeps one song in a JSON state file (STATE). You change it with commands.
The user reviews it in a browser UI, can edit it there, and sends feedback to you.
The final result is a .probundle file that ProPresenter imports.

WORKFLOW
  1. Write the lyrics in the lyrics format (below) to a file, then:
       probundle new song.json --title "Song title" --artist "Artist" --lyrics lyrics.txt
     This creates groups, slides and the arrangement "Standard" (Blank, then each group once).
  2. Set the real song order:
       probundle arrangement set song.json Standard Blank "Verse 1" Chorus "Verse 2" Chorus Bridge Chorus
  3. Check:      probundle show song.json      (prints issues too)
  4. Open the UI for the user:
       probundle ui song.json
  5. Wait for the user:
       probundle feedback song.json --wait
     It returns when the user sends feedback, approves, or exports. It also lists edits the
     user made in the UI. Apply the requested changes with commands, then wait again.
     The UI updates by itself after each command. Exit code 2 = timeout; run it again.
  6. When the user approves, export:
       probundle export song.json [OUT.probundle]
     (The user can also export from the UI.)

LYRICS FORMAT
  [Verse 1]
  First line of slide 1
  Second line of slide 1

  First line of slide 2
  Second line of slide 2

  [Chorus]
  ...

  - "[Name]" starts a group. Blank lines separate slides. Each line is a line break on the slide.
  - Write each group ONCE, with all its slides. Repeats belong in arrangements, not in the lyrics.
  - Do not write the Blank group; it is built in (empty slide, worship look macro, background video).

HOUSE STYLE
  - Group names: Verse 1, Verse 2, ..., Pre-Chorus, Chorus, Bridge, Tag, Intro, Outro, Ending.
    Verse 1, Chorus and Bridge link to the church library groups automatically.
  - One group per song section, with several slides in it.
  - At most %d lines per slide, and at most about %d characters per line (60 pt text box).
  - Write lyrics in normal case; ProPresenter shows them in capitals.
  - Arrangements start with Blank.
  - Background: choose with `+"`probundle set song.json background NAME`"+`; list names with `+"`probundle backgrounds`"+`.

COMMANDS
  probundle help                 all commands with arguments
  probundle show STATE --json    the song as JSON (same shape as op song.replace takes)
  probundle show STATE --lyrics  the groups in the lyrics format
  In slide text arguments, \n is a line break. Use - to read text from stdin.
  Positions and slide numbers start at 1.

OPS (probundle op STATE NAME 'JSON' runs any of them; the UI uses the same ops)
%s`, song.MaxSlideLines, song.MaxLineChars, ops.String())
}
