package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"minimap-lab/internal/input"
)

func inputServer(t testing.TB) (*server, *input.DryEmitter) {
	t.Helper()
	em := &input.DryEmitter{Window: input.Window{PID: 42, Path: "/Applications/Tibia.app"}}
	s := &server{dir: t.TempDir(), gate: make(chan struct{}, 1), driver: input.NewDriver(em, input.DefaultMaxObservationAgeMS)}
	return s, em
}

func postInput(t testing.TB, s *server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "http://127.0.0.1:8095"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	return w
}

func armSession(t testing.TB, s *server) string {
	t.Helper()
	w := postInput(t, s, "/api/arm", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("arm: %d %s", w.Code, w.Body.String())
	}
	var st input.ArmState
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	return st.Session
}

func TestInputAPIWalksAfterArming(t *testing.T) {
	s, em := inputServer(t)
	session := armSession(t, s)

	w := postInput(t, s, "/api/input", `{"session":"`+session+`","seq":1,"action":"walk","direction":"E","observation_age_ms":80}`)

	var got input.Result
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != http.StatusOK || got.Status != "emitted" || got.Key != "numpad6" {
		t.Fatalf("%d %+v", w.Code, got)
	}
	if len(em.Events()) != 1 {
		t.Fatalf("got %v", em.Events())
	}
}

func TestInputAPIRefusesWithoutSessionToken(t *testing.T) {
	s, em := inputServer(t)
	armSession(t, s)

	w := postInput(t, s, "/api/input", `{"seq":1,"action":"walk","direction":"E","observation_age_ms":80}`)

	var got input.Result
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Status != "refused" {
		t.Fatalf("%d %+v", w.Code, got)
	}
	if len(em.Events()) != 0 {
		t.Error("a tokenless request must never reach the system")
	}
}

func TestInputAPIRefusesCrossOriginRequest(t *testing.T) {
	s, em := inputServer(t)
	session := armSession(t, s)
	r := httptest.NewRequest("POST", "http://127.0.0.1:8095/api/input",
		strings.NewReader(`{"session":"`+session+`","seq":1,"action":"walk","direction":"E","observation_age_ms":80}`))
	r.Header.Set("Origin", "https://obca-strona.example")
	w := httptest.NewRecorder()

	s.routes().ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
	if len(em.Events()) != 0 {
		t.Error("127.0.0.1 is not a security boundary; any page can POST to it")
	}
}

func TestInputAPIRejectsMalformedJSON(t *testing.T) {
	s, _ := inputServer(t)
	armSession(t, s)

	w := postInput(t, s, "/api/input", `{"session":`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d", w.Code)
	}
}

func TestInputAPIStatusActsAsHeartbeat(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	r := httptest.NewRequest("GET", "http://127.0.0.1:8095/api/input/status", nil)
	r.Header.Set("X-Input-Session", session)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)

	var st input.ArmState
	json.Unmarshal(w.Body.Bytes(), &st)
	if w.Code != http.StatusOK || !st.Armed {
		t.Fatalf("%d %+v", w.Code, st)
	}
	if st.Session != "" {
		t.Error("status must not echo the token back; it is a bearer secret")
	}
}

func TestInputAPIStatusAvailableWithoutEmitter(t *testing.T) {
	s := &server{dir: t.TempDir(), gate: make(chan struct{}, 1)}

	r := httptest.NewRequest("GET", "http://127.0.0.1:8095/api/input/status", nil)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var body struct {
		Available bool   `json:"available"`
		Platform  string `json:"platform"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Available {
		t.Error("available must be false with no driver")
	}
	if body.Platform == "" {
		t.Error("platform must be reported even with no driver")
	}
}

func TestInputAPIDisarmStops(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	postInput(t, s, "/api/disarm", `{"session":"`+session+`"}`)

	w := postInput(t, s, "/api/input", `{"session":"`+session+`","seq":1,"action":"walk","direction":"E","observation_age_ms":80}`)
	var got input.Result
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
}

func TestInputAPICalibrateStoresPlayerTile(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	w := postInput(t, s, "/api/input/calibrate", `{"session":"`+session+`","x":0.5,"y":0.4}`)

	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if !s.driver.HasTile {
		t.Error("calibration did not reach the driver")
	}
}

func TestInputAPIConfigStoresHotkeys(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	w := postInput(t, s, "/api/input/config",
		`{"session":"`+session+`","keys":{"rope":"f7","hole":"f8"},"click_after_hotkey":true}`)

	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if s.driver.ActionKeys["rope"] != "f7" || s.driver.ActionKeys["hole"] != "f8" {
		t.Fatalf("got %+v", s.driver.ActionKeys)
	}
	if !s.driver.ClickAfterHotkey {
		t.Error("click_after_hotkey did not reach the driver")
	}
}

func TestInputAPIConfigRefusesUnknownKey(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	w := postInput(t, s, "/api/input/config", `{"session":"`+session+`","keys":{"rope":"control"}}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
	// The body must be JSON carrying the reason, not http.Error's plain text:
	// the panel's postSafe() always calls response.json(), and a plain-text
	// body would fail to parse there, silently discarding the actual reason
	// (which field was rejected) behind a generic parse error instead.
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not JSON: %v (%q)", err, w.Body.String())
	}
	if !strings.Contains(body.Reason, "rope") {
		t.Errorf("got reason %q, want it to name the rejected action", body.Reason)
	}
}

func TestInputAPIConfigStoresDirections(t *testing.T) {
	s, em := inputServer(t)
	session := armSession(t, s)

	w := postInput(t, s, "/api/input/config",
		`{"session":"`+session+`","keys":{},"directions":{"N":"w","S":"s","W":"a","E":"d"}}`)

	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if s.driver.DirectionKeys["N"] != "w" {
		t.Fatalf("got %+v", s.driver.DirectionKeys)
	}
	// The real proof the config took effect end to end, the same way the
	// hotkey test above proves it via Submit rather than just the field.
	got := s.driver.Submit(input.Intent{Session: session, Seq: 1, Action: "walk", Direction: "N", AgeMS: 50})
	if got.Status != "emitted" || got.Key != "w" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 1 || em.Events()[0] != "tap w 35ms" {
		t.Fatalf("got %v", em.Events())
	}
}

func TestInputAPIConfigRefusesUnknownDirectionName(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	w := postInput(t, s, "/api/input/config", `{"session":"`+session+`","directions":{"UP":"w"}}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not JSON: %v (%q)", err, w.Body.String())
	}
	if !strings.Contains(body.Reason, "UP") {
		t.Errorf("got reason %q, want it to name the rejected direction", body.Reason)
	}
	// The built-in numpad default must survive a refused, all-or-nothing config.
	if s.driver.DirectionKeys["N"] != "numpad8" {
		t.Error("a refused direction config must not clear the previous mapping")
	}
}

func TestInputAPIConfigRefusesUnknownDirectionKey(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	w := postInput(t, s, "/api/input/config", `{"session":"`+session+`","directions":{"N":"control"}}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
}

// A blank direction field is the deliberate WASD-with-no-diagonal case, not
// an error: the config is accepted, and it is only walking in that direction
// that later gets refused.
func TestInputAPIConfigAcceptsEmptyDirection(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	w := postInput(t, s, "/api/input/config", `{"session":"`+session+`","directions":{"NE":""}}`)

	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	got := s.driver.Submit(input.Intent{Session: session, Seq: 1, Action: "walk", Direction: "NE", AgeMS: 50})
	if got.Status != "refused" || !strings.Contains(got.Reason, "NE") {
		t.Fatalf("got %+v, want a refusal naming the unconfigured direction", got)
	}
}

func TestInputAPIConfigRequiresSessionToken(t *testing.T) {
	s, _ := inputServer(t)
	armSession(t, s)

	w := postInput(t, s, "/api/input/config", `{"keys":{"rope":"f7"}}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
	if s.driver.ActionKeys["rope"] != "" {
		t.Error("a tokenless config request must never reach the driver")
	}
}

// sessionOK is the only guard on /api/disarm, /api/input/calibrate and
// /api/input/done. Every existing test for it uses an empty token; this one
// closes the gap a mutated sessionOK that always returns true would leave
// open by using a well-formed but wrong one instead.
func TestInputAPIDisarmRefusesWrongSessionToken(t *testing.T) {
	s, _ := inputServer(t)
	armSession(t, s)

	w := postInput(t, s, "/api/disarm", `{"session":"podrobiona-sesja"}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
	if !s.driver.Status().Armed {
		t.Error("a wrong session token must not be able to disarm")
	}
}

func TestInputAPIUnavailableWithoutEmitter(t *testing.T) {
	s := &server{dir: t.TempDir(), gate: make(chan struct{}, 1)}

	w := postInput(t, s, "/api/input", `{"seq":1,"action":"walk","direction":"E","observation_age_ms":80}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
}

func TestSelectEmitterRejectsUnknownMode(t *testing.T) {
	if _, err := input.SelectEmitter("wlaczone"); err == nil {
		t.Error("unknown -input value must be refused at start, not silently ignored")
	}
	if em, err := input.SelectEmitter("off"); err != nil || em != nil {
		t.Errorf("off must leave the panel without a driver, got %v %v", em, err)
	}
}
