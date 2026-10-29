// Package server runs the local review UI. The browser calls the same song ops as the CLI,
// so the user and the LLM change one shared state file.
package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"probundle/internal/bundle"
	"probundle/internal/song"
	"probundle/internal/store"
)

//go:embed ui/index.html
var indexHTML []byte

// sidecar is written next to the state file while the server runs.
type sidecar struct {
	PID int    `json:"pid"`
	URL string `json:"url"`
}

func sidecarPath(state string) string { return state + ".ui.json" }

// DefaultExportPath is the last export path, or <title>.probundle next to the state file.
func DefaultExportPath(state string, st *store.State) string {
	if st.ExportPath != "" {
		return st.ExportPath
	}
	dir, _ := filepath.Abs(filepath.Dir(state))
	return filepath.Join(dir, bundle.NormalizeName(bundle.FileTitle(st.Song.Title))+".probundle")
}

type view struct {
	Revision          int           `json:"revision"`
	Song              song.Song     `json:"song"`
	Issues            []song.Issue  `json:"issues"`
	Events            []store.Event `json:"events"`
	Delivered         int           `json:"delivered"`
	ExportPath        string        `json:"export_path"`
	Backgrounds       []string      `json:"backgrounds"`
	DefaultBackground string        `json:"default_background"`
	LibraryGroups     []string      `json:"library_groups"`
	StatePath         string        `json:"state_path"`
	MaxLineChars      int           `json:"max_line_chars"`
	MaxSlideLines     int           `json:"max_slide_lines"`
	Slide             slideStyle    `json:"slide"`
}

// slideStyle lets the UI draw previews with the template's text box and font.
type slideStyle struct {
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	W          float64 `json:"w"`
	H          float64 `json:"h"`
	FontFamily string  `json:"font_family"`
	FontSize   float64 `json:"font_size"`
}

func templateSlideStyle(t *bundle.Template) slideStyle {
	st := slideStyle{Width: 1920, Height: 1080}
	for _, a := range t.LyricCue.GetActions() {
		if size := a.GetSlide().GetPresentation().GetBaseSlide().GetSize(); size != nil {
			st.Width, st.Height = size.GetWidth(), size.GetHeight()
		}
	}
	e := bundle.TextElement(t.LyricCue)
	b := e.GetBounds()
	st.X, st.Y = b.GetOrigin().GetX(), b.GetOrigin().GetY()
	st.W, st.H = b.GetSize().GetWidth(), b.GetSize().GetHeight()
	f := e.GetText().GetAttributes().GetFont()
	st.FontFamily, st.FontSize = f.GetFamily(), f.GetSize()
	return st
}

type srv struct {
	state  string
	origin string
	tmpl   *bundle.Template
}

func (s *srv) view(st *store.State) view {
	v := view{
		Revision:      st.Revision,
		Song:          st.Song,
		Issues:        bundle.Validate(st.Song),
		Delivered:     st.Delivered,
		ExportPath:    DefaultExportPath(s.state, st),
		StatePath:     s.state,
		MaxLineChars:  song.MaxLineChars,
		MaxSlideLines: song.MaxSlideLines,
		Slide:         templateSlideStyle(s.tmpl),
	}
	if v.Issues == nil {
		v.Issues = []song.Issue{}
	}
	events := st.Events
	if len(events) > 100 {
		events = events[len(events)-100:]
	}
	v.Events = events
	for i, b := range s.tmpl.Backgrounds {
		v.Backgrounds = append(v.Backgrounds, b.GetName())
		if i == 0 {
			v.DefaultBackground = b.GetName()
		}
	}
	for _, g := range s.tmpl.Library {
		v.LibraryGroups = append(v.LibraryGroups, g.GetName())
	}
	return v
}

// Run serves the UI until SIGINT/SIGTERM or until the state file disappears.
func Run(state, addr string) error {
	abs, err := filepath.Abs(state)
	if err != nil {
		return err
	}
	if _, err := store.Load(abs); err != nil {
		return err
	}
	tmpl, err := bundle.LoadTemplate()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	origin := "http://" + ln.Addr().String()
	s := &srv{state: abs, origin: origin, tmpl: tmpl}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(indexHTML)
	})
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"state": abs}) })
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/op", s.handleOp)
	mux.HandleFunc("POST /api/feedback", s.handleFeedback)
	mux.HandleFunc("POST /api/export", s.handleExport)

	hs := &http.Server{Handler: s.guard(mux), ReadHeaderTimeout: 10 * time.Second}
	sc := sidecar{PID: os.Getpid(), URL: origin + "/"}
	data, _ := json.Marshal(sc)
	if err := os.WriteFile(sidecarPath(abs), data, 0o644); err != nil {
		return err
	}
	defer os.Remove(sidecarPath(abs))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := os.Stat(abs); errors.Is(err, os.ErrNotExist) {
					stop()
					return
				}
			}
		}
	}()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		hs.Shutdown(shutdown)
	}()
	fmt.Printf("serving %s for %s\n", sc.URL, abs)
	if err := hs.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// guard rejects state changes from other web sites (the server listens on localhost only).
func (s *srv) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			if o := r.Header.Get("Origin"); o != "" && o != s.origin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

// handleState long-polls: with ?after=N it waits up to 25 s for a revision other than N.
func (s *srv) handleState(w http.ResponseWriter, r *http.Request) {
	after, hasAfter := -1, r.URL.Query().Has("after")
	if hasAfter {
		after, _ = strconv.Atoi(r.URL.Query().Get("after"))
	}
	deadline := time.Now().Add(25 * time.Second)
	for {
		st, err := store.Load(s.state)
		if err != nil {
			writeErr(w, err)
			return
		}
		if !hasAfter || st.Revision != after || time.Now().After(deadline) {
			writeJSON(w, 200, s.view(st))
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func (s *srv) handleOp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Op   string          `json:"op"`
		Args json.RawMessage `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	st, err := store.Update(s.state, func(st *store.State) error {
		msg, err := song.Apply(&st.Song, req.Op, req.Args)
		if err != nil {
			return err
		}
		st.AddEvent("ui", "edit", msg, nil)
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, s.view(st))
}

func (s *srv) handleFeedback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind   string        `json:"kind"`
		Text   string        `json:"text"`
		Target *store.Target `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	switch {
	case req.Kind != "feedback" && req.Kind != "approve":
		writeErr(w, fmt.Errorf("kind must be feedback or approve"))
		return
	case req.Kind == "feedback" && req.Text == "":
		writeErr(w, fmt.Errorf("feedback text is empty"))
		return
	case req.Kind == "approve" && req.Text == "":
		req.Text = "The user approved the song."
	}
	st, err := store.Update(s.state, func(st *store.State) error {
		st.AddEvent("ui", req.Kind, req.Text, req.Target)
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, s.view(st))
}

func (s *srv) handleExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	st, err := store.Load(s.state)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := strings.TrimSpace(req.Path)
	if out == "" {
		out = DefaultExportPath(s.state, st)
	}
	if strings.HasPrefix(out, "~/") {
		home, _ := os.UserHomeDir()
		out = filepath.Join(home, out[2:])
	}
	if !strings.HasSuffix(out, ".probundle") {
		out += ".probundle"
	}
	if err := bundle.Export(st.Song, out); err != nil {
		writeErr(w, err)
		return
	}
	abs, _ := filepath.Abs(out)
	st, err = store.Update(s.state, func(st *store.State) error {
		st.ExportPath = abs
		st.AddEvent("ui", "export", "The user exported "+abs, nil)
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, s.view(st))
}

func readSidecar(state string) (*sidecar, error) {
	data, err := os.ReadFile(sidecarPath(state))
	if err != nil {
		return nil, err
	}
	var sc sidecar
	if err := json.Unmarshal(data, &sc); err != nil {
		return nil, err
	}
	return &sc, nil
}

func alive(sc *sidecar) bool {
	if sc == nil || syscall.Kill(sc.PID, 0) != nil {
		return false
	}
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(sc.URL + "api/ping")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// Launch starts the UI server in the background if it is not running, and opens the browser.
func Launch(state string, openBrowser bool) (url string, started bool, err error) {
	abs, err := filepath.Abs(state)
	if err != nil {
		return "", false, err
	}
	if _, err := store.Load(abs); err != nil {
		return "", false, err
	}
	sc, _ := readSidecar(abs)
	if !alive(sc) {
		os.Remove(sidecarPath(abs))
		exe, err := os.Executable()
		if err != nil {
			return "", false, err
		}
		logf, err := os.OpenFile(abs+".ui.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return "", false, err
		}
		defer logf.Close()
		cmd := exec.Command(exe, "serve", abs)
		cmd.Stdout, cmd.Stderr = logf, logf
		// A new session keeps the server alive after the calling shell exits.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return "", false, err
		}
		pid := cmd.Process.Pid
		cmd.Process.Release()
		deadline := time.Now().Add(5 * time.Second)
		for {
			sc, _ = readSidecar(abs)
			if sc != nil && sc.PID == pid && alive(sc) {
				break
			}
			if time.Now().After(deadline) {
				return "", false, fmt.Errorf("UI server did not start; see %s.ui.log", abs)
			}
			time.Sleep(100 * time.Millisecond)
		}
		started = true
	}
	if openBrowser {
		if err := exec.Command("open", sc.URL).Run(); err != nil {
			return sc.URL, started, fmt.Errorf("server runs at %s but the browser did not open: %w", sc.URL, err)
		}
	}
	return sc.URL, started, nil
}

// Stop ends a running UI server for this state file.
func Stop(state string) (bool, error) {
	abs, err := filepath.Abs(state)
	if err != nil {
		return false, err
	}
	sc, err := readSidecar(abs)
	if err != nil || !alive(sc) {
		os.Remove(sidecarPath(abs))
		return false, nil
	}
	if err := syscall.Kill(sc.PID, syscall.SIGTERM); err != nil {
		return false, err
	}
	for range 50 {
		if syscall.Kill(sc.PID, 0) != nil {
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true, fmt.Errorf("server %d did not stop", sc.PID)
}
