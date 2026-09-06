package main

import (
	"fmt"
	"net/http"
)

// Bits of one preview tile. Missing data and a wall are separate on purpose:
// merged, every unvisited area would look like solid rock.
const (
	gridBlocked = 1 << iota
	gridMissing
	gridTemp
	gridPerm
)

// grid answers the panel's live walkability window. It uses its own small
// cache rather than the route planner's: the two ask for very different
// rectangles, and sharing one cache would make them evict each other on every
// single reading.
func (s *server) grid(w http.ResponseWriter, r *http.Request) {
	x, y, z, radius, ok := windowParams(r)
	if !ok {
		http.Error(w, "Wymagane x, y (0–65535), z (0–15) i r (1–64).", http.StatusBadRequest)
		return
	}
	area := rectAround(x, y, radius)
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	if s.previewCache == nil || s.previewFloor != z || !area.In(s.previewCache.bounds) {
		g, err := loadCostArea(s.dir, z, area)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.previewCache, s.previewFloor = g, z
	}
	grid := s.previewCache
	overlay := s.blocks.Snapshot(area, z)

	side := 2*radius + 1
	out := make([]byte, side*side)
	for row := 0; row < side; row++ {
		for col := 0; col < side; col++ {
			wx, wy := area.Min.X+col, area.Min.Y+row
			var b byte
			if !grid.Covered(wx, wy) {
				b |= gridMissing
			} else if grid.At(wx, wy) == blockedCost {
				b |= gridBlocked
			}
			switch overlay.Tile(wx, wy) {
			case KindTemp:
				b |= gridTemp
			case KindPerm:
				b |= gridPerm
			}
			out[row*side+col] = b
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Grid-Origin", fmt.Sprintf("%d,%d", area.Min.X, area.Min.Y))
	w.Header().Set("X-Grid-Revision", fmt.Sprintf("%d", s.blocks.Revision()))
	w.Write(out)
}
