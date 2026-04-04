package navigation

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func createTestMinimap(t *testing.T, dir string) {
	t.Helper()
	chunks := [][2]int{{32000, 32000}, {32256, 32000}, {32000, 32256}}
	for _, chunk := range chunks {
		cx, cy := chunk[0], chunk[1]
		seed := int64(cx + cy)
		rng := rand.New(rand.NewSource(seed))

		colorImg := image.NewRGBA(image.Rect(0, 0, 256, 256))
		for y := 0; y < 256; y++ {
			for x := 0; x < 256; x++ {
				colorImg.SetRGBA(x, y, color.RGBA{
					R: uint8(rng.Intn(256)),
					G: uint8(rng.Intn(256)),
					B: uint8(rng.Intn(256)),
					A: 255,
				})
			}
		}
		fColor, err := os.Create(filepath.Join(dir, fmt.Sprintf("Minimap_Color_%d_%d_7.png", cx, cy)))
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(fColor, colorImg)
		fColor.Close()

		costImg := image.NewGray(image.Rect(0, 0, 256, 256))
		for y := 0; y < 256; y++ {
			for x := 0; x < 256; x++ {
				if x < 10 || y < 10 {
					costImg.SetGray(x, y, color.Gray{Y: 255})
				} else {
					costImg.SetGray(x, y, color.Gray{Y: 150})
				}
			}
		}
		fCost, err := os.Create(filepath.Join(dir, fmt.Sprintf("Minimap_WaypointCost_%d_%d_7.png", cx, cy)))
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(fCost, costImg)
		fCost.Close()
	}
}

func TestAtlasLoad(t *testing.T) {
	dir := t.TempDir()
	createTestMinimap(t, dir)
	atlas := NewAtlas(dir)
	if err := atlas.Load(); err != nil {
		t.Fatal(err)
	}
	if len(atlas.ColorChunks) != 3 {
		t.Errorf("ColorChunks len = %d, want 3", len(atlas.ColorChunks))
	}
	if len(atlas.CostChunks) != 3 {
		t.Errorf("CostChunks len = %d, want 3", len(atlas.CostChunks))
	}
}

func TestAtlasGetCostAt(t *testing.T) {
	dir := t.TempDir()
	createTestMinimap(t, dir)
	atlas := NewAtlas(dir)
	atlas.Load()

	cost := atlas.GetCostAt(32000+5, 32000+5, 7)
	if cost != 255 {
		t.Errorf("cost at border = %d, want 255", cost)
	}
	cost = atlas.GetCostAt(32000+100, 32000+100, 7)
	if cost != 150 {
		t.Errorf("cost at interior = %d, want 150", cost)
	}
}

func TestAtlasGetCostAtMissingChunk(t *testing.T) {
	dir := t.TempDir()
	createTestMinimap(t, dir)
	atlas := NewAtlas(dir)
	atlas.Load()
	cost := atlas.GetCostAt(99999, 99999, 7)
	if cost != 255 {
		t.Errorf("cost at missing = %d, want 255", cost)
	}
}

func TestAtlasZLevels(t *testing.T) {
	dir := t.TempDir()
	createTestMinimap(t, dir)
	atlas := NewAtlas(dir)
	atlas.Load()
	if _, ok := atlas.ZLevels[7]; !ok {
		t.Error("z-level 7 not found")
	}
}

func TestAtlasChunkKeysForZ(t *testing.T) {
	dir := t.TempDir()
	createTestMinimap(t, dir)
	atlas := NewAtlas(dir)
	atlas.Load()
	keys := atlas.ChunkKeysForZ(7)
	if len(keys) != 3 {
		t.Errorf("keys len = %d, want 3", len(keys))
	}
}

func TestAtlasWorldBounds(t *testing.T) {
	dir := t.TempDir()
	createTestMinimap(t, dir)
	atlas := NewAtlas(dir)
	atlas.Load()
	bounds := atlas.WorldBounds(7)
	if bounds.MinX != 32000 {
		t.Errorf("MinX = %d, want 32000", bounds.MinX)
	}
	if bounds.MinY != 32000 {
		t.Errorf("MinY = %d, want 32000", bounds.MinY)
	}
	if bounds.MaxX != 32256+256 {
		t.Errorf("MaxX = %d, want %d", bounds.MaxX, 32256+256)
	}
	if bounds.MaxY != 32256+256 {
		t.Errorf("MaxY = %d, want %d", bounds.MaxY, 32256+256)
	}
}
