package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
)

type matchRequest struct {
	locate.Request
	NoPreview bool `json:"no_preview,omitempty"`
}

func (s *server) info(w http.ResponseWriter, r *http.Request) {
	floors := []int{}
	seen := map[int]bool{}
	entries, err := os.ReadDir(s.dir)
	for _, e := range entries {
		if _, _, z, ok := mapdata.ParseChunkName(e.Name()); ok {
			if z >= 0 && z <= 15 && !seen[z] {
				seen[z] = true
				floors = append(floors, z)
			}
		}
	}
	sort.Ints(floors)
	message := ""
	if err != nil {
		message = "Brak katalogu map. Demo działa; własne mapy podaj przez -maps."
	}
	writeJSON(w, map[string]any{"floors": floors, "maps": s.dir, "message": message})
}

func (s *server) match(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "Nieprawidłowy formularz lub plik >8 MB", 400)
		return
	}
	defer r.MultipartForm.RemoveAll()
	var req matchRequest
	if err := json.Unmarshal([]byte(r.FormValue("options")), &req); err != nil {
		http.Error(w, "Nieprawidłowe opcje lub piętro", 400)
		return
	}
	if err := req.Request.Validate(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	f, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Brak obrazu", 400)
		return
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width > 1024 || cfg.Height > 1024 || cfg.Width < 8 || cfg.Height < 8 {
		http.Error(w, "Wycinek PNG/JPEG musi mieć 8–1024 px na bok", 400)
		return
	}
	if _, err = f.Seek(0, 0); err != nil {
		http.Error(w, "Nie można odczytać obrazu", 400)
		return
	}
	im, _, err := image.Decode(f)
	if err != nil {
		http.Error(w, "Nie można odczytać obrazu", 400)
		return
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		http.Error(w, "Trwa inne wyszukiwanie. Spróbuj ponownie.", 429)
		return
	}
	// Throttle disk writes and logging during 5-10 Hz tracking.
	debugNow := s.debugDir != "" && !req.Demo && (req.Near == nil || time.Since(s.lastDebug) >= time.Second)
	if debugNow {
		s.lastDebug = time.Now()
		var capture bytes.Buffer
		png.Encode(&capture, im)
		s.saveDebug("last-input.png", capture.Bytes())
		s.saveDebug("last-options.json", []byte(r.FormValue("options")))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	result, atlas, err := s.locator.Locate(ctx, im, req.Request)
	if err != nil {
		if debugNow {
			s.saveDebug("last-result.json", []byte(fmt.Sprintf("%q", err.Error())))
		}
		code := 400
		if ctx.Err() != nil {
			code = 408
		}
		http.Error(w, fmt.Sprintf("Wyszukiwanie przerwane: %v", err), code)
		return
	}
	result.ElapsedMS = time.Since(started).Milliseconds()
	if debugNow {
		data, _ := json.MarshalIndent(result, "", "  ")
		s.saveDebug("last-result.json", data)
		log.Printf("locate floor=%d zoom=%d crop=%dx%d found=%v best=%+v competitor=%+v elapsed=%dms",
			req.Floor, req.Zoom, im.Bounds().Dx(), im.Bounds().Dy(), result.Found, result.Best, result.Competitor, result.ElapsedMS)
	}
	preview := ""
	if result.Best != nil && atlas != nil && !req.NoPreview {
		preview = "data:image/png;base64," + base64.StdEncoding.EncodeToString(previewPNG(atlas, result.Best.X, result.Best.Y))
	}
	writeJSON(w, struct {
		locate.Result
		Preview string `json:"preview"`
	}{result, preview})
}

// previewPNG cuts a 129x129-tile window out of the atlas, centred on the
// match, with a crosshair on the middle tile.
func previewPNG(atlas *mapdata.Atlas, x, y int) []byte {
	p := image.Pt(x, y).Sub(atlas.Origin)
	patch := image.NewNRGBA(image.Rect(0, 0, 129, 129))
	draw.Draw(patch, patch.Bounds(), atlas.Image, p.Sub(image.Pt(64, 64)), draw.Src)
	for d := -5; d <= 5; d++ {
		patch.Set(64+d, 64, color.NRGBA{255, 60, 90, 255})
		patch.Set(64, 64+d, color.NRGBA{255, 60, 90, 255})
	}
	var buf bytes.Buffer
	png.Encode(&buf, patch)
	return buf.Bytes()
}
