package vision

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/tibiabot/internal/navigation"
	"gocv.io/x/gocv"
)

func createLocatorAtlas(t *testing.T) *navigation.Atlas {
	t.Helper()
	dir := t.TempDir()
	chunks := [][2]int{{32000, 32000}, {32256, 32000}, {32000, 32256}}
	for _, chunk := range chunks {
		cx, cy := chunk[0], chunk[1]
		seed := int64(cx*1000 + cy)
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
		fColor, _ := os.Create(filepath.Join(dir, fmt.Sprintf("Minimap_Color_%d_%d_7.png", cx, cy)))
		png.Encode(fColor, colorImg)
		fColor.Close()

		costImg := image.NewGray(image.Rect(0, 0, 256, 256))
		for y := 0; y < 256; y++ {
			for x := 0; x < 256; x++ {
				costImg.SetGray(x, y, color.Gray{Y: 150})
			}
		}
		fCost, _ := os.Create(filepath.Join(dir, fmt.Sprintf("Minimap_WaypointCost_%d_%d_7.png", cx, cy)))
		png.Encode(fCost, costImg)
		fCost.Close()
	}
	atlas := navigation.NewAtlas(dir)
	atlas.Load()
	return atlas
}

// rgbaToTestMat converts *image.RGBA to a GoCV Mat for testing.
func rgbaToTestMat(img *image.RGBA) gocv.Mat {
	return rgbaToMat(img)
}

func TestLocatorFindPosition(t *testing.T) {
	atlas := createLocatorAtlas(t)
	locator := NewMinimapLocator(atlas)
	defer locator.Close()

	tile, ok := atlas.GetColorTile(32000, 32000, 7)
	if !ok {
		t.Fatal("tile not found")
	}

	// Convert tile to GoCV mat, extract snippet
	tileMat := rgbaToTestMat(tile)
	defer tileMat.Close()
	snippetRegion := tileMat.Region(image.Rect(80, 80, 130, 130))
	snippetClone := snippetRegion.Clone()
	defer snippetClone.Close()

	x, y, confidence, found := locator.Locate(snippetClone, 7)
	if !found {
		t.Fatal("expected match")
	}
	if intAbs(x-(32000+80)) > 2 {
		t.Errorf("x = %d, want ~%d", x, 32000+80)
	}
	if intAbs(y-(32000+80)) > 2 {
		t.Errorf("y = %d, want ~%d", y, 32000+80)
	}
	if confidence < 0.7 {
		t.Errorf("confidence = %f, want > 0.7", confidence)
	}
}

func TestLocatorNarrowedSearch(t *testing.T) {
	atlas := createLocatorAtlas(t)
	locator := NewMinimapLocator(atlas)
	defer locator.Close()

	tile, _ := atlas.GetColorTile(32000, 32000, 7)
	tileMat := rgbaToTestMat(tile)
	defer tileMat.Close()

	region1 := tileMat.Region(image.Rect(100, 100, 150, 150))
	snippet1 := region1.Clone()
	locator.Locate(snippet1, 7)
	snippet1.Close()

	region2 := tileMat.Region(image.Rect(105, 105, 155, 155))
	snippet2 := region2.Clone()
	x, y, _, found := locator.Locate(snippet2, 7)
	snippet2.Close()

	if !found {
		t.Fatal("expected match on narrowed search")
	}
	if intAbs(x-(32000+105)) > 2 {
		t.Errorf("x = %d, want ~%d", x, 32000+105)
	}
	if intAbs(y-(32000+105)) > 2 {
		t.Errorf("y = %d, want ~%d", y, 32000+105)
	}
}

func TestLocatorNoMatch(t *testing.T) {
	atlas := createLocatorAtlas(t)
	locator := NewMinimapLocator(atlas)
	defer locator.Close()

	snippet := gocv.NewMatWithSize(50, 50, gocv.MatTypeCV8UC3)
	defer snippet.Close()
	for r := 0; r < 50; r++ {
		for c := 0; c < 50; c++ {
			snippet.SetUCharAt(r, c*3+0, 128)
			snippet.SetUCharAt(r, c*3+1, 128)
			snippet.SetUCharAt(r, c*3+2, 128)
		}
	}
	_, _, _, found := locator.Locate(snippet, 7, 0.99)
	if found {
		t.Error("expected no match for uniform snippet")
	}
}

func intAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
