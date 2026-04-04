package navigation

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func createPathfindingAtlas(t *testing.T) *Atlas {
	t.Helper()
	dir := t.TempDir()

	// Cost: border is 255, interior (50-200) is 150
	costImg := image.NewGray(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			costImg.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	for y := 50; y < 200; y++ {
		for x := 50; x < 200; x++ {
			costImg.SetGray(x, y, color.Gray{Y: 150})
		}
	}
	fCost, _ := os.Create(filepath.Join(dir, "Minimap_WaypointCost_32000_32000_7.png"))
	png.Encode(fCost, costImg)
	fCost.Close()

	// Color tile (black)
	colorImg := image.NewRGBA(image.Rect(0, 0, 256, 256))
	fColor, _ := os.Create(filepath.Join(dir, "Minimap_Color_32000_32000_7.png"))
	png.Encode(fColor, colorImg)
	fColor.Close()

	atlas := NewAtlas(dir)
	atlas.Load()
	return atlas
}

func TestFindPathSimple(t *testing.T) {
	atlas := createPathfindingAtlas(t)
	start := [3]int{32000 + 60, 32000 + 60, 7}
	goal := [3]int{32000 + 190, 32000 + 190, 7}
	path := FindPath(atlas, start, goal, 100000)
	if path == nil {
		t.Fatal("expected path, got nil")
	}
	if len(path) == 0 {
		t.Fatal("expected non-empty path")
	}
	if path[0] != [2]int{start[0], start[1]} {
		t.Errorf("first = %v, want %v", path[0], [2]int{start[0], start[1]})
	}
	if path[len(path)-1] != [2]int{goal[0], goal[1]} {
		t.Errorf("last = %v, want %v", path[len(path)-1], [2]int{goal[0], goal[1]})
	}
}

func TestFindPathBlocked(t *testing.T) {
	atlas := createPathfindingAtlas(t)
	start := [3]int{32000 + 60, 32000 + 60, 7}
	goal := [3]int{32000 + 10, 32000 + 10, 7}
	path := FindPath(atlas, start, goal, 100000)
	if path != nil {
		t.Error("expected nil path for blocked goal")
	}
}

func TestFindPathAdjacent(t *testing.T) {
	atlas := createPathfindingAtlas(t)
	start := [3]int{32000 + 100, 32000 + 100, 7}
	goal := [3]int{32000 + 101, 32000 + 101, 7}
	path := FindPath(atlas, start, goal, 100000)
	if path == nil {
		t.Fatal("expected path")
	}
	if len(path) != 2 {
		t.Errorf("len = %d, want 2", len(path))
	}
}

func TestFindPathSamePosition(t *testing.T) {
	atlas := createPathfindingAtlas(t)
	pos := [3]int{32000 + 100, 32000 + 100, 7}
	path := FindPath(atlas, pos, pos, 100000)
	if path == nil {
		t.Fatal("expected path")
	}
	if len(path) != 1 {
		t.Errorf("len = %d, want 1", len(path))
	}
}

func TestFindPathUsesDiagonal(t *testing.T) {
	atlas := createPathfindingAtlas(t)
	start := [3]int{32000 + 60, 32000 + 60, 7}
	goal := [3]int{32000 + 70, 32000 + 70, 7}
	path := FindPath(atlas, start, goal, 100000)
	if path == nil {
		t.Fatal("expected path")
	}
	if len(path) > 12 {
		t.Errorf("len = %d, want <= 12 (diagonal)", len(path))
	}
}
