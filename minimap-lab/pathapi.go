package main

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"net/http"
	"time"
)

const (
	defaultPathMargin = 64
	maxPathMargin     = 256
	pathTimeout       = 5 * time.Second
	// Coordinates alone are not a bound: two valid tiles at opposite map
	// corners would allocate gigabytes before any search starts.
	maxSearchTiles = 4 << 20
)

// Every coordinate is a pointer so a missing field is refused rather than
// silently read as tile zero.
type tileRef struct {
	X *int `json:"x"`
	Y *int `json:"y"`
	Z *int `json:"z"`
}

type pathRequest struct {
	From   *tileRef `json:"from"`
	To     *tileRef `json:"to"`
	Margin int      `json:"margin,omitempty"`
}

func (t *tileRef) position() (Position, bool) {
	if t == nil || t.X == nil || t.Y == nil || t.Z == nil {
		return Position{}, false
	}
	p := Position{*t.X, *t.Y, *t.Z}
	return p, p.X >= 0 && p.X <= 65535 && p.Y >= 0 && p.Y <= 65535 && p.Z >= 0 && p.Z <= 15
}

// path answers a single route query. It deliberately avoids server.gate: the
// 10 Hz locate loop must never wait behind a route search.
func (s *server) path(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	var req pathRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	// More() only reports a following *value*; a stray bracket or a second
	// document after padding needs the decoder to actually reach the end.
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "Nieprawidłowe żądanie JSON", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		http.Error(w, "Treść żądania musi zawierać dokładnie jeden dokument JSON.", http.StatusBadRequest)
		return
	}
	from, fromOK := req.From.position()
	to, toOK := req.To.position()
	if !fromOK || !toOK || req.Margin < 0 || req.Margin > maxPathMargin {
		http.Error(w, "Wymagane pełne pola from/to ze współrzędnymi 0–65535, piętrem 0–15 i marginesem 0–256.", http.StatusBadRequest)
		return
	}
	if from.Z != to.Z {
		writeJSON(w, PathResult{Status: "different_floor", Steps: [][2]int{},
			Reason: "Waypoint leży na innym piętrze. Użyj przejścia i poczekaj na potwierdzenie nowego Z."})
		return
	}
	margin := req.Margin
	if margin == 0 {
		margin = defaultPathMargin
	}
	area := image.Rect(min(from.X, to.X), min(from.Y, to.Y),
		max(from.X, to.X)+1, max(from.Y, to.Y)+1).Inset(-margin)
	if int64(area.Dx())*int64(area.Dy()) > maxSearchTiles {
		http.Error(w, "Obszar wyszukiwania jest za duży. Zmniejsz odległość między waypointami lub margines.", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), pathTimeout)
	defer cancel()

	s.costMu.Lock()
	defer s.costMu.Unlock()
	// The lock itself cannot be cancelled, so a request abandoned while waiting
	// is dropped here instead of going on to scan and decode chunks.
	if err := ctx.Err(); err != nil {
		http.Error(w, "Żądanie porzucone.", http.StatusRequestTimeout)
		return
	}
	grid, err := s.costGrid(from.Z, area)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Every reachable tile is closed at most once, so the area itself bounds
	// the work; no arbitrary iteration constant is needed.
	result := findPath(ctx, grid.limitTo(area), [2]int{from.X, from.Y}, [2]int{to.X, to.Y}, area.Dx()*area.Dy())
	if result.Steps == nil {
		result.Steps = [][2]int{}
	}
	result.ElapsedMS = float64(time.Since(started).Microseconds()) / 1000
	writeJSON(w, result)
}

// costGrid keeps one decoded floor of walking costs. Called under costMu.
func (s *server) costGrid(floor int, area image.Rectangle) (*CostGrid, error) {
	if s.costCache != nil && s.costFloor == floor && area.In(s.costCache.bounds) {
		return s.costCache, nil
	}
	grid, err := loadCostArea(s.dir, floor, area)
	if err != nil {
		return nil, err
	}
	s.costCache, s.costFloor = grid, floor
	return grid, nil
}
