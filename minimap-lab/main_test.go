package main

import (
	"bytes"
	"image/png"
	"io/fs"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"
)

// TestEveryReferencedAssetIsServed follows what the page actually asks for -
// the tags in index.html, the import specifiers of every module, and the
// worker path - and checks each one comes back. Walking the embedded FS
// instead would be circular: it could only prove that what go:embed took is
// served, never that what the page needs was taken.
//
// The panel is a graph of ES modules now, and a missing one is silent: the
// browser 404s on the import and renders a blank page with nothing in the Go
// tests to say which file. Confirmed by sabotage - a single mistyped import
// specifier fails this test and names both the missing path and the file that
// asked for it.
//
// (A file named with a leading "_" is not the hazard it looks like: the
// exclusion of "_" and "." applies to embedding a directory, while the glob in
// "//go:embed web/*" matches such a name and takes it. Verified by serving
// one.)
func TestEveryReferencedAssetIsServed(t *testing.T) {
	var (
		htmlRef   = regexp.MustCompile(`(?:src|href)="(/[^":]+)"`)
		jsImport  = regexp.MustCompile(`from\s+'(\./[^']+)'`)
		jsLiteral = regexp.MustCompile(`'(/[\w.-]+\.(?:js|css))'`)
	)

	web, err := fs.Sub(assets, "web")
	if err != nil {
		t.Fatal(err)
	}
	// wanted maps an asset path to the file that asked for it, so a failure
	// names the reference and not just the missing file.
	wanted := map[string]string{}

	index, err := fs.ReadFile(web, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range htmlRef.FindAllStringSubmatch(string(index), -1) {
		wanted[strings.TrimPrefix(m[1], "/")] = "index.html"
	}
	if len(wanted) < 5 {
		t.Fatalf("index.html odwołuje się do %d zasobów; wzorzec przestał pasować", len(wanted))
	}

	scripts, err := fs.Glob(web, "*.js")
	if err != nil {
		t.Fatal(err)
	}
	imports := 0
	for _, name := range scripts {
		src, err := fs.ReadFile(web, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range jsImport.FindAllStringSubmatch(string(src), -1) {
			wanted[path.Join(path.Dir(name), m[1])] = name
			imports++
		}
		for _, m := range jsLiteral.FindAllStringSubmatch(string(src), -1) {
			wanted[strings.TrimPrefix(m[1], "/")] = name
		}
	}
	if imports < 10 {
		t.Fatalf("znaleziono %d importów między modułami; wzorzec przestał pasować", imports)
	}

	s := newServer(t.TempDir())
	for asset, from := range wanted {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8095/"+asset, nil)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Errorf("GET /%s (z %s): %d, chciano 200", asset, from, w.Code)
		}
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
