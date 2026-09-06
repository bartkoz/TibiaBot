package main

import (
	"encoding/json"
	"image"
	"net/http"
	"strconv"

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/nav"
)

// maxBlocksRadius keeps one listing bounded. The panel never asks for more
// than its own preview window.
const maxBlocksRadius = 64

// rectAround is the square window shared by the blocks listing and the grid
// preview, so both agree on what "radius" means.
func rectAround(x, y, r int) image.Rectangle {
	return image.Rect(x-r, y-r, x+r+1, y+r+1)
}

func (s *server) observeBlock(w http.ResponseWriter, r *http.Request) {
	if s.blocks == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Magazyn blokad nie jest włączony.")
		return
	}
	var obs nav.Observation
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&obs); err != nil {
		http.Error(w, "Nieprawidłowe żądanie JSON", http.StatusBadRequest)
		return
	}
	// A character standing on water or inside a wall is not a character - it is
	// a bad match from the locator. The tile it "failed to enter" is then
	// somewhere else entirely, and learning from it writes a permanent block
	// into open ground.
	//
	// Revocations are exempt: "the character walked onto this tile" can only
	// ever remove a block, so a bogus one costs a forgotten lesson, while
	// refusing them would let a wrong permanent block survive exactly where the
	// map data is poor.
	if obs.Outcome != "entered" && !s.standable(obs.From) {
		writeJSON(w, nav.Decision{Result: "ignored", Revision: s.blocks.Revision(),
			Reason: "Pozycja postaci wypada na kratce nieprzechodniej, więc odczyt jest niewiarygodny; nic nie zapisano."})
		return
	}
	d := s.blocks.Observe(obs)
	// Only a change worth keeping touches the disk; an ignored observation or a
	// temporary block must not rewrite the file.
	if d.Result == "promoted" || d.Result == "cleared" {
		s.blocks.Flush()
	}
	writeJSON(w, d)
}

func (s *server) listBlocks(w http.ResponseWriter, r *http.Request) {
	if s.blocks == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Magazyn blokad nie jest włączony.")
		return
	}
	x, y, z, radius, ok := windowParams(r)
	if !ok {
		http.Error(w, "Wymagane x, y (0–65535), z (0–15) i r (1–64).", http.StatusBadRequest)
		return
	}
	writeJSON(w, s.blocks.List(rectAround(x, y, radius), z))
}

func (s *server) deleteBlock(w http.ResponseWriter, r *http.Request) {
	if s.blocks == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Magazyn blokad nie jest włączony.")
		return
	}
	var p mapdata.Position
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&p); err != nil {
		http.Error(w, "Nieprawidłowe żądanie JSON", http.StatusBadRequest)
		return
	}
	cleared := s.blocks.Clear(p)
	if cleared {
		s.blocks.Flush()
	}
	writeJSON(w, map[string]bool{"cleared": cleared})
}

// standable reports whether the map data allows a character to be on this
// tile. Missing data counts as standable: absence of evidence is not evidence
// of a bad reading, and refusing to learn anywhere the pack is thin would be
// worse than the false positives this guards against.
func (s *server) standable(p mapdata.Position) bool {
	area := rectAround(p.X, p.Y, 1)
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	if s.previewCache == nil || s.previewFloor != p.Z || !area.In(s.previewCache.Bounds()) {
		g, err := mapdata.LoadCostArea(s.dir, p.Z, area.Inset(-previewMargin))
		if err != nil {
			return true // cannot tell; do not block learning on a read error
		}
		s.previewCache, s.previewFloor = g, p.Z
	}
	if !s.previewCache.Covered(p.X, p.Y) {
		return true
	}
	return s.previewCache.At(p.X, p.Y) != mapdata.BlockedCost
}

// windowParams parses the x/y/z/r query shared by the listing and the preview.
func windowParams(r *http.Request) (x, y, z, radius int, ok bool) {
	q := r.URL.Query()
	x, err1 := strconv.Atoi(q.Get("x"))
	y, err2 := strconv.Atoi(q.Get("y"))
	z, err3 := strconv.Atoi(q.Get("z"))
	radius, err4 := strconv.Atoi(q.Get("r"))
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return 0, 0, 0, 0, false
	}
	if x < 0 || x > 65535 || y < 0 || y > 65535 || z < 0 || z > 15 || radius < 1 || radius > maxBlocksRadius {
		return 0, 0, 0, 0, false
	}
	return x, y, z, radius, true
}
