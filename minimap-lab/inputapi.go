package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
)

// selectEmitter turns the -input flag into an emitter. "off" leaves the panel
// in the dry-run behaviour it had before this feature existed.
func selectEmitter(mode string) (Emitter, error) {
	switch mode {
	case "off":
		return nil, nil
	case "dry":
		return &DryEmitter{Window: Window{PID: 1, Path: "dry", Title: "dry"}}, nil
	case "system":
		return newSystemEmitter()
	default:
		return nil, fmt.Errorf("-input przyjmuje off, dry albo system")
	}
}

// readInput refuses early when control is switched off, then decodes the body.
func (s *server) readInput(w http.ResponseWriter, r *http.Request, v any) bool {
	if s.driver == nil {
		http.Error(w, "Sterowanie wyłączone. Uruchom panel z -input dry albo -input system.", http.StatusServiceUnavailable)
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(v); err != nil {
		http.Error(w, "Nieprawidłowe żądanie JSON", http.StatusBadRequest)
		return false
	}
	return true
}

// sessionOK guards the routes that change state on behalf of an armed panel.
func (s *server) sessionOK(w http.ResponseWriter, session string) bool {
	if session == "" || session != s.driver.Status().Session {
		http.Error(w, "Nieprawidłowy token sesji.", http.StatusForbidden)
		return false
	}
	return true
}

func (s *server) arm(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if !s.readInput(w, r, &body) {
		return
	}
	state, err := s.driver.Arm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, state)
}

func (s *server) disarm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string `json:"session"`
	}
	if !s.readInput(w, r, &body) || !s.sessionOK(w, body.Session) {
		return
	}
	s.driver.Disarm("zatrzymane z panelu")
	writeJSON(w, s.driver.Status())
}

func (s *server) input(w http.ResponseWriter, r *http.Request) {
	var in Intent
	if !s.readInput(w, r, &in) {
		return
	}
	result := s.driver.Submit(in)
	s.logIntent(in, result)
	writeJSON(w, result)
}

// logIntent records what the panel asked for and what the driver did with it.
// The panel's status line is overwritten by the heartbeat within 200 ms, so
// without this the reason a step was refused is effectively invisible - which
// is exactly what a user sees when the character will not move.
func (s *server) logIntent(in Intent, r InputResult) {
	line := fmt.Sprintf("%s %s%s -> %s", in.Action, in.Direction, in.Type, r.Status)
	if r.Key != "" {
		line += " " + r.Key
	}
	if r.Reason != "" {
		line += ": " + r.Reason
	}
	line += fmt.Sprintf(" (wiek %d ms)", in.AgeMS)
	s.repeatMu.Lock()
	defer s.repeatMu.Unlock()
	if line == s.lastLogged {
		s.repeats++
		// A refusal repeating at the tracking rate would drown the log, so
		// only every tenth identical line is printed, carrying the count.
		if s.repeats%10 != 0 {
			return
		}
		log.Printf("%s [x%d]", line, s.repeats)
		return
	}
	s.lastLogged, s.repeats = line, 1
	log.Print(line)
}

func (s *server) calibrate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string  `json:"session"`
		X       float64 `json:"x"`
		Y       float64 `json:"y"`
	}
	if !s.readInput(w, r, &body) || !s.sessionOK(w, body.Session) {
		return
	}
	if err := s.driver.Calibrate(body.X, body.Y); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *server) inputConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session          string            `json:"session"`
		Keys             map[string]string `json:"keys"`
		ClickAfterHotkey bool              `json:"click_after_hotkey"`
		Directions       map[string]string `json:"directions"`
	}
	if !s.readInput(w, r, &body) || !s.sessionOK(w, body.Session) {
		return
	}
	if err := s.driver.SetInputConfig(body.Keys, body.ClickAfterHotkey, body.Directions); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *server) actionDone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string `json:"session"`
	}
	if !s.readInput(w, r, &body) || !s.sessionOK(w, body.Session) {
		return
	}
	s.driver.ActionDone()
	writeJSON(w, map[string]bool{"ok": true})
}

// inputStatus doubles as the heartbeat: the panel polls it every 200 ms, and a
// gap longer than heartbeatTimeoutMS disarms the driver.
func (s *server) inputStatus(w http.ResponseWriter, r *http.Request) {
	if s.driver == nil {
		writeJSON(w, map[string]any{"available": false, "platform": runtime.GOOS})
		return
	}
	state := s.driver.Beat(r.Header.Get("X-Input-Session"))
	// The token is a bearer secret; it leaves the server once, in the arm reply.
	state.Session = ""
	writeJSON(w, state)
}
