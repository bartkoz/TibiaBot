package main

import (
	"bytes"
	"image/png"
	"net/http/httptest"
	"testing"
)

func TestHTTPDemoRoundTrip(t *testing.T) {
	s := newServer(t.TempDir())
	r := httptest.NewRequest("GET", "http://127.0.0.1:8095/api/demo", nil)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPValidationAndStaticPanel(t *testing.T) {
	s := newServer(t.TempDir())
	for _, tc := range []struct {
		method, path, origin string
		code                 int
	}{
		{"GET", "http://127.0.0.1:8095/", "", 200},
		{"GET", "http://127.0.0.1:8095/panel.js", "", 200},
		{"GET", "http://127.0.0.1:8095/camera.js", "", 200},
		{"GET", "http://127.0.0.1:8095/worker.js", "", 200},
		{"GET", "http://127.0.0.1:8095/api/info", "", 200},
		{"POST", "http://127.0.0.1:8095/api/path", "", 400},
		{"POST", "http://127.0.0.1:8095/api/path", "https://example.org", 403},
		{"GET", "http://example.org:8095/", "", 403},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s %s: got %d want %d", tc.method, tc.path, w.Code, tc.code)
		}
	}
}
