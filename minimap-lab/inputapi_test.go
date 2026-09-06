package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func inputServer(t testing.TB) (*server, *DryEmitter) {
	t.Helper()
	em := &DryEmitter{Window: Window{PID: 42, Path: "/Applications/Tibia.app"}}
	s := &server{dir: t.TempDir(), gate: make(chan struct{}, 1), driver: NewDriver(em)}
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
	var st ArmState
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	return st.Session
}

func TestInputAPIWalksAfterArming(t *testing.T) {
	s, em := inputServer(t)
	session := armSession(t, s)

	w := postInput(t, s, "/api/input", `{"session":"`+session+`","seq":1,"action":"walk","direction":"E","observation_age_ms":80}`)

	var got InputResult
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

	var got InputResult
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

	r := httptest.NewRequest("GET", "http://127.0.0.1:8095/api/input/status?session="+session, nil)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)

	var st ArmState
	json.Unmarshal(w.Body.Bytes(), &st)
	if w.Code != http.StatusOK || !st.Armed {
		t.Fatalf("%d %+v", w.Code, st)
	}
	if st.Session != "" {
		t.Error("status must not echo the token back; it is a bearer secret")
	}
}

func TestInputAPIDisarmStops(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	postInput(t, s, "/api/disarm", `{"session":"`+session+`"}`)

	w := postInput(t, s, "/api/input", `{"session":"`+session+`","seq":1,"action":"walk","direction":"E","observation_age_ms":80}`)
	var got InputResult
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

func TestInputAPIUnavailableWithoutEmitter(t *testing.T) {
	s := &server{dir: t.TempDir(), gate: make(chan struct{}, 1)}

	w := postInput(t, s, "/api/input", `{"seq":1,"action":"walk","direction":"E","observation_age_ms":80}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
}

func TestSelectEmitterRejectsUnknownMode(t *testing.T) {
	if _, err := selectEmitter("wlaczone"); err == nil {
		t.Error("unknown -input value must be refused at start, not silently ignored")
	}
	if em, err := selectEmitter("off"); err != nil || em != nil {
		t.Errorf("off must leave the panel without a driver, got %v %v", em, err)
	}
}
