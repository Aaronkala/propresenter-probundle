// Package store keeps the working state of one song in a JSON file.
// The CLI and the UI server both change the file, so every change takes an exclusive lock.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"probundle/internal/song"
)

const Format = "probundle-song/1"

type State struct {
	Format     string    `json:"format"`
	Revision   int       `json:"revision"`
	Song       song.Song `json:"song"`
	ExportPath string    `json:"export_path,omitempty"`
	Events     []Event   `json:"events"`
	// Delivered is the last event ID that `probundle feedback` gave to the LLM.
	Delivered int `json:"delivered"`
}

// Event records what the user did in the UI, so the LLM can read it later.
type Event struct {
	ID     int       `json:"id"`
	Time   time.Time `json:"time"`
	Source string    `json:"source"` // "ui" or "cli"
	Kind   string    `json:"kind"`   // "edit", "feedback", "approve", "export"
	Text   string    `json:"text"`
	Target *Target   `json:"target,omitempty"`
}

// Target points feedback at one slide or group. Slide 0 means the whole group.
type Target struct {
	Group string `json:"group"`
	Slide int    `json:"slide,omitempty"`
}

func (t *Target) String() string {
	if t == nil {
		return ""
	}
	if t.Slide > 0 {
		return fmt.Sprintf("%s slide %d", t.Group, t.Slide)
	}
	return t.Group
}

func (s *State) AddEvent(source, kind, text string, target *Target) Event {
	id := 1
	if n := len(s.Events); n > 0 {
		id = s.Events[n-1].ID + 1
	}
	e := Event{ID: id, Time: time.Now().UTC().Truncate(time.Second), Source: source, Kind: kind, Text: text, Target: target}
	s.Events = append(s.Events, e)
	return e
}

// Undelivered returns events the LLM has not read yet.
func (s *State) Undelivered() []Event {
	var out []Event
	for _, e := range s.Events {
		if e.ID > s.Delivered {
			out = append(out, e)
		}
	}
	return out
}

func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if st.Format != Format {
		return nil, fmt.Errorf("%s: format %q is not %q", path, st.Format, Format)
	}
	return &st, nil
}

// Create writes a new state file. It fails if the file exists.
func Create(path string, st *State) error {
	st.Format = Format
	if st.Events == nil {
		st.Events = []Event{}
	}
	unlock, err := lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	return write(path, st)
}

// ErrNoChange tells Update to skip the write and keep the revision.
var ErrNoChange = errors.New("no change")

// Update loads the state under the lock, runs fn and saves the result with the next revision.
func Update(path string, fn func(*State) error) (*State, error) {
	unlock, err := lock(path)
	if err != nil {
		return nil, err
	}
	defer unlock()
	st, err := Load(path)
	if err != nil {
		return nil, err
	}
	if err := fn(st); err != nil {
		if errors.Is(err, ErrNoChange) {
			return st, nil
		}
		return nil, err
	}
	st.Revision++
	return st, write(path, st)
}

func lock(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

// write replaces the file in one rename, so readers never see a half-written file.
func write(path string, st *State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".probundle-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
