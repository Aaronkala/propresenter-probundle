---
name: probundle
description: This skill should be used when the user asks to "make a probundle", "create a ProPresenter song", "add a song to ProPresenter", "make slides for a worship song", "build a .probundle file", or gives song lyrics to turn into ProPresenter slides. It drives the `probundle` CLI, opens a review UI for the user, and exports a .probundle file that ProPresenter imports.
---

# probundle: ProPresenter song bundles

`probundle` makes ProPresenter `.probundle` song files. The agent writes the song with CLI commands.
The user reviews and edits it in a browser UI and sends feedback. The agent changes the song until
the user approves, then exports the bundle.

Source and docs: `~/Projects/propresenter-probundle` (README, `docs/probundle-format.md`).

## 1. Prepare

1. Check the tool: `command -v probundle`. If it is missing, run
   `go install ./cmd/probundle` in `~/Projects/propresenter-probundle`.
2. Read the full workflow, the lyrics format, the house style and all ops: `probundle guide`.
   The guide is the reference. This skill does not repeat it.
3. Choose a folder for the song. Use the folder the user names. If the user names no folder, use
   `~/Desktop/probundle-songs/<Song title>/`. The state file (`song.json`) and the export go there.

## 2. Get the lyrics

- Use only lyrics that the user gives (text, file, or a link the user supplies). Do not write lyrics
  from memory and do not fill in missing lines. If the lyrics are incomplete, ask the user.
- Keep the language, spelling and punctuation of the source. Write in normal case; ProPresenter shows capitals.
- Find the song structure (verses, pre-chorus, chorus, bridge, tag). Write each section once in the
  lyrics file. Put repeats in the arrangement, not in the lyrics.
- Split each section into slides of 1 or 2 lines. Break lines at natural phrase ends.
  `probundle show` warns about lines that are too long for the text box.
- Get the title and the artist. Ask the user if they are not clear.

## 3. Build and review

Run the steps from `probundle guide`:

```sh
probundle new song.json --title "…" --artist "…" --lyrics lyrics.txt
probundle arrangement set song.json Standard Blank "Verse 1" Chorus …
probundle show song.json          # fix every error; fix warnings when possible
probundle ui song.json            # opens the browser for the user
```

Then tell the user that the UI is open and what to do there: edit, comment on slides,
**Send feedback**, **Approve**, or **Export**.

## 4. Feedback loop

- Run `probundle feedback song.json --wait --timeout <T>` with `T` shorter than the shell tool time limit
  (for example `--timeout 4m` when the limit is 5 minutes). Exit code 2 means a timeout and no feedback:
  run the same command again. Do not end the turn while the user reviews, unless the user says so.
- The output lists the user's edits and messages. Run `probundle show song.json` before you change
  anything, because the user can change the song in the UI.
- Apply each request with CLI commands (`slide set`, `slide add`, `group rename`, `arrangement set`,
  or `probundle op` for any op). Do not edit `song.json` by hand: the file has a lock and a revision counter.
- Tell the user in the chat what changed, then wait again.
- An `approve` event means: export now. An `export` event means the user already exported; report the path.

## 5. Finish

```sh
probundle export song.json                     # prints the .probundle path
probundle inspect "<that path>"                # manifest check must say OK
probundle ui song.json --stop                  # stop the UI server
```

Report the bundle path to the user. Tell the user to import it into ProPresenter on the
church Mac. The background video is not in the bundle. ProPresenter finds it on the church Mac in
`~/Documents/MotionBackgrounds2026/`.

## Other tasks

- Edit an existing song: `probundle new song.json --from OLD.probundle`. This merges old one-slide groups.
- Look inside any bundle: `probundle inspect FILE.probundle [--json]`.
- List the background videos: `probundle backgrounds`. Set one with `probundle set song.json background "<name>"`.

## Problems

| Symptom | Action |
|---------|--------|
| `export` fails with "song has errors" | Run `probundle validate song.json` and fix each error. |
| `ui` says the server did not start | Read `song.json.ui.log` in the song folder. |
| The UI shows "disconnected" | Run `probundle ui song.json` again; it starts a new server and opens the browser. |
| A group or arrangement name is wrong in a command | Names are exact and case-sensitive; check `probundle show song.json`. |
