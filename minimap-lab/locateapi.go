package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
)

// A manual reading is independent of the movement driver and capture session.
func (s *server) locate(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil || r.MultipartForm == nil {
		writeJSONError(w, http.StatusBadRequest, "Nieprawidłowy lub zbyt duży wycinek minimapy.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	var req locate.Request
	if err := json.Unmarshal([]byte(r.FormValue("options")), &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Nieprawidłowe ustawienia dopasowania.")
		return
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Brak wycinka minimapy.")
		return
	}
	defer file.Close()
	config, err := png.DecodeConfig(file)
	if err != nil || config.Width < 8 || config.Height < 8 || config.Width > 1024 || config.Height > 1024 {
		writeJSONError(w, http.StatusBadRequest, "Wycinek PNG musi mieć 8–1024 px na bok.")
		return
	}
	if _, err := file.Seek(0, 0); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Nie można odczytać wycinka.")
		return
	}
	im, err := png.Decode(file)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Nieprawidłowy obraz PNG.")
		return
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		writeJSONError(w, http.StatusConflict, "Wyszukiwanie już trwa.")
		return
	}
	if s.debugDir != "" && !req.Demo {
		var capture bytes.Buffer
		if err := png.Encode(&capture, im); err == nil {
			s.saveDebug("manual-input.png", capture.Bytes())
		}
		s.saveDebug("manual-options.json", []byte(r.FormValue("options")))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	result, atlas, err := s.locator.Locate(ctx, im, req)
	result.ElapsedMS = time.Since(started).Milliseconds()
	if s.debugDir != "" && !req.Demo {
		var data []byte
		if err != nil {
			data, _ = json.Marshal(map[string]any{"reason": err.Error(), "elapsed_ms": result.ElapsedMS})
		} else {
			data, _ = json.MarshalIndent(result, "", "  ")
		}
		s.saveDebug("manual-result.json", data)
	}
	log.Printf("manual locate floor=%d zoom=%d crop=%dx%d found=%v elapsed=%dms error=%v",
		req.Floor, req.Zoom, config.Width, config.Height, result.Found, result.ElapsedMS, err)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeJSONError(w, http.StatusRequestTimeout, "Pełne wyszukiwanie przekroczyło limit 45 s. Sprawdź piętro Z i ustaw ręcznie skalę minimapy, aby pominąć próby Auto.")
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	preview := ""
	if result.Found && result.Position != nil && atlas != nil {
		preview = "data:image/png;base64," + base64.StdEncoding.EncodeToString(previewPNG(atlas, result.Position.X, result.Position.Y))
	}
	writeJSON(w, struct {
		locate.Result
		Preview string `json:"preview,omitempty"`
	}{result, preview})
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
	writeJSON(w, map[string]any{"floors": floors, "maps": s.dir, "message": message, "control_available": s.driver != nil})
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
