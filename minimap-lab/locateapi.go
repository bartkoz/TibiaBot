package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"os"
	"sort"

	"minimap-lab/internal/mapdata"
)

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
