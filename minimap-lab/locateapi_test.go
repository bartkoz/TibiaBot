package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"testing"
	"time"

	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
)

func TestManualLocateWithoutDriver(t *testing.T) {
	s := newServer(t.TempDir())
	var picture bytes.Buffer
	if err := png.Encode(&picture, mapdata.DemoSnippet(mapdata.DemoAtlas())); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, options string
		image         []byte
		code          int
	}{
		{"auto demo", `{"demo":true,"floor":7,"zoom":0,"marker_x":94,"marker_y":94,"mask_radius":5,"min_score":0.85,"min_gap":0.015}`, picture.Bytes(), 200},
		{"bad options", `{`, picture.Bytes(), 400},
		{"bad image", `{}`, []byte("broken"), 400},
		{"bad floor", `{"demo":true,"floor":16}`, picture.Bytes(), 400},
		{"deadline", `{"demo":true,"floor":7}`, picture.Bytes(), 408},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			if err := form.WriteField("options", tc.options); err != nil {
				t.Fatal(err)
			}
			part, err := form.CreateFormFile("image", "minimap.png")
			if err != nil {
				t.Fatal(err)
			}
			part.Write(tc.image)
			form.Close()
			r := httptest.NewRequest("POST", "http://127.0.0.1:8095/api/locate", &body)
			if tc.name == "deadline" {
				ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(-time.Second))
				defer cancel()
				r = r.WithContext(ctx)
			}
			r.Header.Set("Content-Type", form.FormDataContentType())
			w := httptest.NewRecorder()
			s.routes().ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
			if tc.name == "deadline" && (bytes.Contains(w.Body.Bytes(), []byte("context deadline exceeded")) || !bytes.Contains(w.Body.Bytes(), []byte("45 s"))) {
				t.Fatalf("unhelpful timeout: %s", w.Body.String())
			}
			if tc.code != 200 {
				return
			}
			var result struct {
				locate.Result
				Preview string `json:"preview"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !result.Found || result.Position == nil || *result.Position != (mapdata.Position{X: 32200, Y: 32180, Z: 7}) || result.Zoom != 2 || result.Preview == "" {
				t.Fatalf("unexpected match: %+v", result.Result)
			}
			if s.driver != nil || s.captureSession() != 0 {
				t.Fatal("manual reading enabled control")
			}
		})
	}
}
