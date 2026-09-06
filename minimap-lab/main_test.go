package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/draw"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/testenv"
)

func TestHTTPDemoRoundTrip(t *testing.T) {
	s := &server{dir: t.TempDir(), gate: make(chan struct{}, 1)}
	r := httptest.NewRequest("GET", "http://127.0.0.1:8095/api/demo", nil)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("image", "demo.png")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(w.Body.Bytes())
	form.WriteField("options", `{"demo":true,"floor":7,"zoom":2,"marker_x":94,"marker_y":94,"mask_radius":5,"min_score":0.94,"min_gap":0.015}`)
	form.Close()
	r = httptest.NewRequest("POST", "http://127.0.0.1:8095/api/locate", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	var result locate.Result
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	assertPosition(t, result, mapdata.Position{X: 32200, Y: 32180, Z: 7})
	if !strings.Contains(w.Body.String(), "data:image/png;base64,") {
		t.Fatal("missing reference preview")
	}
}

func trackingBody(t testing.TB, req matchRequest) ([]byte, string) {
	t.Helper()
	data := testenv.FixtureBytes(t, "venore-capture.png")
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("image", "capture.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	opts, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("options", string(opts)); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), form.FormDataContentType()
}

func replayTracking(handler http.Handler, body []byte, contentType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://127.0.0.1:8095/api/locate", bytes.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestHTTPTracking(t *testing.T) {
	a, _, o := actualTrackingFixture(t)
	s := &server{dir: t.TempDir(), gate: make(chan struct{}, 1), cached: a, debugDir: t.TempDir()}
	req := matchRequest{Options: o, Floor: 7, Near: &mapdata.Position{X: 32958, Y: 32077, Z: 7}, Radius: 5, NoPreview: true}
	body, ct := trackingBody(t, req)
	w := replayTracking(s.routes(), body, ct)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var result locate.Result
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	assertPosition(t, result, mapdata.Position{X: 32958, Y: 32077, Z: 7})
	if result.Mode != "local" || result.SearchPositions != 121 || result.MatchMS <= 0 {
		t.Fatalf("missing tracking metrics: %+v", result)
	}
	if strings.Contains(w.Body.String(), "data:image/png") {
		t.Fatal("unrequested preview generated")
	}
	for _, change := range []func(*matchRequest){func(r *matchRequest) { r.Radius = 65 }, func(r *matchRequest) { r.Floor = 9 }, func(r *matchRequest) { r.Zoom = 0 }} {
		invalid := req
		change(&invalid)
		body, ct = trackingBody(t, invalid)
		w = replayTracking(s.routes(), body, ct)
		if w.Code != 400 {
			t.Fatalf("invalid local request accepted: %+v", invalid)
		}
	}
}

func BenchmarkHTTPTrackActualCapture(b *testing.B) {
	ref := testenv.LoadFixture(b, "venore-reference.png")
	rgba := image.NewNRGBA(ref.Bounds())
	draw.Draw(rgba, rgba.Bounds(), ref, ref.Bounds().Min, draw.Src)
	a := &mapdata.Atlas{Image: rgba, Origin: image.Pt(32768, 32000), Floor: 7}
	s := &server{gate: make(chan struct{}, 1), cached: a}
	handler := s.routes()
	req := matchRequest{Options: locate.Options{Zoom: 1, MarkerX: 52, MarkerY: 57, MaskRadius: 5, MinScore: .85, MinGap: .015}, Floor: 7, Near: &mapdata.Position{X: 32958, Y: 32077, Z: 7}, Radius: 5, NoPreview: true}
	body, ct := trackingBody(b, req)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := replayTracking(handler, body, ct)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"found":true`) {
			b.Fatalf("failed: %s", w.Body.String())
		}
	}
}

func TestHTTPValidationAndStaticPanel(t *testing.T) {
	s := &server{dir: t.TempDir(), gate: make(chan struct{}, 1)}
	for _, tc := range []struct {
		method, path, origin string
		code                 int
	}{
		{"GET", "http://127.0.0.1:8095/", "", 200},
		{"GET", "http://127.0.0.1:8095/app.js", "", 200},
		{"GET", "http://127.0.0.1:8095/api/info", "", 200},
		{"POST", "http://127.0.0.1:8095/api/locate", "", 400},
		{"POST", "http://127.0.0.1:8095/api/locate", "https://example.org", 403},
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
