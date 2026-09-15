package main

import (
	"bytes"
	"image/png"
	"io/fs"
	"net/http/httptest"
	"testing"
)

// TestEveryEmbeddedAssetIsServed walks what go:embed actually took rather than
// naming files: the panel is a graph of ES modules now, and a list written by
// hand would go stale the moment one more module joins it. A module that fails
// to load leaves a blank page and no Go test with anything to say about it.
func TestEveryEmbeddedAssetIsServed(t *testing.T) {
	s := newServer(t.TempDir())
	web, err := fs.Sub(assets, "web")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := fs.WalkDir(web, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		count++
		r := httptest.NewRequest("GET", "http://127.0.0.1:8095/"+path, nil)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		// FileServer canonicalises /index.html to /, which the table test
		// above already fetches; every other asset must come back whole.
		if path == "index.html" {
			if w.Code != 301 {
				t.Errorf("GET /index.html: got %d want 301", w.Code)
			}
			return nil
		}
		if w.Code != 200 {
			t.Errorf("GET /%s: got %d want 200", path, w.Code)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("go:embed nie wciągnął żadnego pliku panelu")
	}
}

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
