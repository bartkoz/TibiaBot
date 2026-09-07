package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/nav"
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

func (t *tileRef) position() (mapdata.Position, bool) {
	if t == nil || t.X == nil || t.Y == nil || t.Z == nil {
		return mapdata.Position{}, false
	}
	p := mapdata.Position{X: *t.X, Y: *t.Y, Z: *t.Z}
	return p, p.X >= 0 && p.X <= 65535 && p.Y >= 0 && p.Y <= 65535 && p.Z >= 0 && p.Z <= 15
}

// path answers a single route query. It deliberately avoids server.gate: the
// tracking loop must never wait behind a route search.
func (s *server) path(w http.ResponseWriter, r *http.Request) {
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
	if !fromOK || !toOK || req.Margin < 0 || req.Margin > nav.MaxMargin {
		http.Error(w, "Wymagane pełne pola from/to ze współrzędnymi 0–65535, piętrem 0–15 i marginesem 0–256.", http.StatusBadRequest)
		return
	}
	result, err := s.planner.Plan(r.Context(), s.blocks, from, to, req.Margin)
	if err != nil {
		if r.Context().Err() != nil {
			http.Error(w, "Żądanie porzucone.", http.StatusRequestTimeout)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, result)
}
