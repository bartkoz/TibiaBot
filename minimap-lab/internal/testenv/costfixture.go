package testenv

import (
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"testing"
)

// Real WaypointCost tiles are paletted PNGs whose palette is identity only up
// to index 250; 251-255 carry marker colors. Reading them as grayscale turns
// the blocked value 255 into ~76, which would look walkable.
func costPalette() color.Palette {
	p := make(color.Palette, 256)
	for i := 0; i < 251; i++ {
		p[i] = color.RGBA{uint8(i), uint8(i), uint8(i), 255}
	}
	p[251] = color.RGBA{255, 0, 0, 255}
	p[252] = color.RGBA{255, 0, 0, 255}
	p[253] = color.RGBA{255, 0, 0, 255}
	p[254] = color.RGBA{255, 0, 255, 255}
	p[255] = color.RGBA{255, 255, 0, 255}
	return p
}

// CostTile builds a 256x256 tile whose every pixel is fill, then applies edits
// keyed by in-tile coordinates.
func CostTile(fill uint8, edits map[[2]int]uint8) *image.Paletted {
	im := image.NewPaletted(image.Rect(0, 0, 256, 256), costPalette())
	for i := range im.Pix {
		im.Pix[i] = fill
	}
	for p, v := range edits {
		im.SetColorIndex(p[0], p[1], v)
	}
	return im
}

func WriteCostTile(t testing.TB, dir string, x, y, z int, im *image.Paletted) {
	t.Helper()
	SavePNG(t, filepath.Join(dir, fmt.Sprintf("Minimap_WaypointCost_%d_%d_%d.png", x, y, z)), im)
}
