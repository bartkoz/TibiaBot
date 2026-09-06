package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	locate.Options
	Floor          int               `json:"floor"`
	Demo           bool              `json:"demo"`
	Near           *mapdata.Position `json:"near,omitempty"`
	Radius         int               `json:"radius,omitempty"`
	NoPreview      bool              `json:"no_preview,omitempty"`
	AdjacentFloors bool              `json:"adjacent_floors,omitempty"`
	FloorRadius    int               `json:"floor_radius,omitempty"`
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
	if err := json.Unmarshal([]byte(r.FormValue("options")), &req); err != nil || req.Floor < 0 || req.Floor > 15 {
		http.Error(w, "Nieprawidłowe opcje lub piętro", 400)
		return
	}
	if req.Near != nil && (req.Near.Z < 0 || req.Near.Z > 15 || abs(req.Near.Z-req.Floor) > 1 || req.Near.X < 0 || req.Near.X > 65535 || req.Near.Y < 0 || req.Near.Y > 65535 || req.Zoom < 1 || req.Zoom > 8 || req.Radius < 1 || req.Radius > 64 || req.FloorRadius < 0 || req.FloorRadius > 32) {
		http.Error(w, "Nieprawidłowy obszar lokalny: wymagane Z lub Z±1, skala 1–8, promień ruchu 1–64 i promień przejścia 1–32 (0 = 8).", 400)
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
	// Throttle disk writes and logging during 5–10 Hz tracking.
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
	var atlas *mapdata.Atlas
	matchStarted := time.Now()
	var result locate.Result
	if req.Demo {
		atlas = mapdata.DemoAtlas()
	} else if req.Near != nil {
		result, atlas, err = s.locateLocal(ctx, im, req)
	} else {
		if s.cached == nil || s.cached.Floor != req.Floor {
			s.cached = nil
			s.cached, err = mapdata.LoadAtlas(s.dir, req.Floor)
		}
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		atlas = s.cached
	}
	if req.Near != nil && req.Demo {
		result, err = locate.Near(ctx, atlas, im, req.Options, *req.Near, req.Radius)
	} else if req.Near == nil {
		result, err = locate.WithScale(ctx, atlas, im, req.Options)
	}
	result.MatchMS = float64(time.Since(matchStarted).Microseconds()) / 1000
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
		log.Printf("locate floor=%d zoom=%d crop=%dx%d found=%v best=%+v competitor=%+v elapsed=%dms", req.Floor, req.Zoom, im.Bounds().Dx(), im.Bounds().Dy(), result.Found, result.Best, result.Competitor, result.ElapsedMS)
	}
	preview := ""
	if result.Best != nil && atlas != nil && !req.NoPreview {
		p := image.Pt(result.Best.X, result.Best.Y).Sub(atlas.Origin)
		patch := image.NewNRGBA(image.Rect(0, 0, 129, 129))
		draw.Draw(patch, patch.Bounds(), atlas.Image, p.Sub(image.Pt(64, 64)), draw.Src)
		for d := -5; d <= 5; d++ {
			patch.Set(64+d, 64, color.NRGBA{255, 60, 90, 255})
			patch.Set(64, 64+d, color.NRGBA{255, 60, 90, 255})
		}
		var buf bytes.Buffer
		png.Encode(&buf, patch)
		preview = "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	}
	writeJSON(w, struct {
		locate.Result
		Preview string `json:"preview"`
	}{result, preview})
}

type localAtlasEntry struct {
	atlas    *mapdata.Atlas
	coverage image.Rectangle
	used     uint64
}

// Called under server.gate. Keep three bounded local atlases in addition to
// the one full atlas used for initial/global localization.
func (s *server) localAtlas(floor int, area image.Rectangle) (*mapdata.Atlas, error) {
	if s.cached != nil && s.cached.Floor == floor {
		return s.cached, nil
	}
	if s.localAtlases == nil {
		s.localAtlases = make(map[int]localAtlasEntry)
	}
	s.cacheClock++
	if entry, ok := s.localAtlases[floor]; ok && area.In(entry.coverage) {
		entry.used = s.cacheClock
		s.localAtlases[floor] = entry
		return entry.atlas, nil
	}
	// Align coverage with tile boundaries, including known missing chunks.
	coverage := image.Rect(area.Min.X&^255, area.Min.Y&^255, (area.Max.X+255)&^255, (area.Max.Y+255)&^255)
	atlas, err := mapdata.LoadAtlasArea(s.dir, floor, &coverage)
	if err != nil && !errors.Is(err, mapdata.ErrNoMapData) {
		return nil, err
	}
	if _, exists := s.localAtlases[floor]; !exists && len(s.localAtlases) >= 3 {
		oldest := -1
		var used uint64
		for z, entry := range s.localAtlases {
			if oldest < 0 || entry.used < used {
				oldest = z
				used = entry.used
			}
		}
		delete(s.localAtlases, oldest)
	}
	s.localAtlases[floor] = localAtlasEntry{atlas, coverage, s.cacheClock}
	return atlas, nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func localFootprint(im image.Image, o locate.Options, near mapdata.Position, radius int) image.Rectangle {
	// Round outward so partially visible enlarged cells and crop seams fit.
	return image.Rect(near.X-radius-(o.MarkerX+o.Zoom-1)/o.Zoom-2,
		near.Y-radius-(o.MarkerY+o.Zoom-1)/o.Zoom-2,
		near.X+radius+(im.Bounds().Dx()-o.MarkerX+o.Zoom-1)/o.Zoom+2,
		near.Y+radius+(im.Bounds().Dy()-o.MarkerY+o.Zoom-1)/o.Zoom+2)
}

// Try the selected floor first. If tracking there fails, compare BOTH adjacent
// floors against each other and the original candidate before accepting Z.
func (s *server) locateLocal(ctx context.Context, im image.Image, req matchRequest) (locate.Result, *mapdata.Atlas, error) {
	floorRadius := req.FloorRadius
	if floorRadius == 0 {
		floorRadius = 8
	}
	var results []locate.Result
	var searched, unavailable []int
	atlases := make(map[int]*mapdata.Atlas)
	searchFloor := func(z, radius int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		searched = append(searched, z)
		near := *req.Near
		near.Z = z
		atlas, err := s.localAtlas(z, localFootprint(im, req.Options, near, radius))
		if err != nil {
			return err
		}
		if atlas == nil {
			unavailable = append(unavailable, z)
			return nil
		}
		atlases[z] = atlas
		r, err := locate.Near(ctx, atlas, im, req.Options, near, radius)
		if err != nil {
			return err
		}
		results = append(results, r)
		return nil
	}
	radius := req.Radius
	if req.Floor != req.Near.Z {
		radius = floorRadius
	}
	if err := searchFloor(req.Floor, radius); err != nil {
		return locate.Result{}, nil, err
	}
	primaryFound := len(results) > 0 && results[0].Found
	if !primaryFound && req.AdjacentFloors && req.Floor == req.Near.Z {
		for _, z := range []int{req.Near.Z - 1, req.Near.Z + 1} {
			if z >= 0 && z <= 15 {
				if err := searchFloor(z, floorRadius); err != nil {
					return locate.Result{}, nil, err
				}
			}
		}
	}
	positions := 0
	for _, r := range results {
		positions += r.SearchPositions
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Best == nil {
			return false
		}
		if results[j].Best == nil {
			return true
		}
		return results[i].Best.Score > results[j].Best.Score
	})
	result := locate.Result{Mode: "local", Zoom: req.Zoom, Reason: "Brak dopasowania w pobliżu ostatniego XY na sprawdzonych piętrach."}
	if len(results) > 0 {
		result = results[0]
	}
	if result.Best != nil && len(results) > 1 && results[1].Best != nil && result.Best.Score-results[1].Best.Score <= req.MinGap {
		result.Found = false
		result.Position = nil
		if result.Competitor == nil || results[1].Best.Score > result.Competitor.Score {
			result.Competitor = results[1].Best
		}
		result.Reason = "Niejednoznaczne piętro: podobne dopasowania na różnych Z. Pozycja pozostaje nieznana."
	}
	result.SearchPositions = positions
	result.SearchedFloors = searched
	result.UnavailableFloors = unavailable
	var atlas *mapdata.Atlas
	if result.Best != nil {
		atlas = atlases[result.Best.Z]
	}
	if result.Found && result.Position.Z != req.Near.Z {
		result.FloorChanged = true
		result.Reason = fmt.Sprintf("Potwierdzono zmianę piętra Z=%d → %d w pobliżu poprzedniego XY.", req.Near.Z, result.Position.Z)
	}
	return result, atlas, nil
}
