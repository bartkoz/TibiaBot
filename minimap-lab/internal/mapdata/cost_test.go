package mapdata

import (
	"image"
	"testing"

	"minimap-lab/internal/testenv"
	"path/filepath"
)

func TestCostGridReadsPaletteIndexNotGray(t *testing.T) {
	dir := t.TempDir()
	testenv.WriteCostTile(t, dir, 32768, 32000, 7, testenv.CostTile(100, map[[2]int]uint8{
		{10, 20}: 255,
		{11, 20}: 150,
	}))

	grid, err := LoadCostArea(dir, 7, image.Rect(32768, 32000, 33024, 32256))
	if err != nil {
		t.Fatal(err)
	}

	if got := grid.At(32778, 32020); got != 255 {
		t.Errorf("blocked tile: got %d, want 255", got)
	}
	if got := grid.At(32779, 32020); got != 150 {
		t.Errorf("costly tile: got %d, want 150", got)
	}
	if got := grid.At(32768, 32000); got != 100 {
		t.Errorf("plain tile: got %d, want 100", got)
	}
}

func TestCostGridTreatsMissingChunkAsBlocked(t *testing.T) {
	dir := t.TempDir()
	testenv.WriteCostTile(t, dir, 32768, 32000, 7, testenv.CostTile(100, nil))

	// The area spans two chunks; only the first one exists on disk.
	grid, err := LoadCostArea(dir, 7, image.Rect(32768, 32000, 33280, 32256))
	if err != nil {
		t.Fatal(err)
	}

	if got := grid.At(33100, 32100); got != BlockedCost {
		t.Errorf("missing chunk: got %d, want %d", got, BlockedCost)
	}
}

func TestCostGridReadsAcrossChunkBoundary(t *testing.T) {
	dir := t.TempDir()
	testenv.WriteCostTile(t, dir, 32768, 32000, 7, testenv.CostTile(255, map[[2]int]uint8{{255, 10}: 100}))
	testenv.WriteCostTile(t, dir, 33024, 32000, 7, testenv.CostTile(255, map[[2]int]uint8{{0, 10}: 110}))

	grid, err := LoadCostArea(dir, 7, image.Rect(33020, 32008, 33028, 32012))
	if err != nil {
		t.Fatal(err)
	}

	if got := grid.At(33023, 32010); got != 100 {
		t.Errorf("last column of left chunk: got %d, want 100", got)
	}
	if got := grid.At(33024, 32010); got != 110 {
		t.Errorf("first column of right chunk: got %d, want 110", got)
	}
}

func TestCostGridBlocksOutsideLoadedArea(t *testing.T) {
	dir := t.TempDir()
	testenv.WriteCostTile(t, dir, 32768, 32000, 7, testenv.CostTile(100, nil))

	grid, err := LoadCostArea(dir, 7, image.Rect(32768, 32000, 33024, 32256))
	if err != nil {
		t.Fatal(err)
	}

	if got := grid.At(40000, 40000); got != BlockedCost {
		t.Errorf("far outside: got %d, want %d", got, BlockedCost)
	}
}

func TestCostGridIgnoresOtherFloors(t *testing.T) {
	dir := t.TempDir()
	testenv.WriteCostTile(t, dir, 32768, 32000, 7, testenv.CostTile(100, nil))
	testenv.WriteCostTile(t, dir, 32768, 32000, 8, testenv.CostTile(120, nil))

	grid, err := LoadCostArea(dir, 8, image.Rect(32768, 32000, 33024, 32256))
	if err != nil {
		t.Fatal(err)
	}

	if got := grid.At(32800, 32050); got != 120 {
		t.Errorf("floor 8: got %d, want 120", got)
	}
}

func TestCostGridMeasuresACheapestTileOfZero(t *testing.T) {
	dir := t.TempDir()
	testenv.WriteCostTile(t, dir, 32768, 32000, 7, testenv.CostTile(100, map[[2]int]uint8{{5, 5}: 0}))

	grid, err := LoadCostArea(dir, 7, image.Rect(32768, 32000, 33024, 32256))
	if err != nil {
		t.Fatal(err)
	}

	if grid.cheapest != 0 || grid.At(32773, 32005) != 0 {
		t.Fatalf("a free tile is walkable: cheapest=%d at=%d", grid.cheapest, grid.At(32773, 32005))
	}
	if !grid.walkable {
		t.Error("a grid with a zero-cost tile has walkable ground")
	}
}

func TestCostGridSkipsUnparsableChunkNames(t *testing.T) {
	dir := t.TempDir()
	testenv.WriteCostTile(t, dir, 32768, 32000, 7, testenv.CostTile(100, nil))
	// A stray file must not take a whole floor down with it.
	testenv.SavePNG(t, filepath.Join(dir, "Minimap_WaypointCost_99999999999999999999_1_7.png"), testenv.CostTile(100, nil))

	grid, err := LoadCostArea(dir, 7, image.Rect(32768, 32000, 33024, 32256))
	if err != nil {
		t.Fatal(err)
	}

	if got := grid.At(32800, 32050); got != 100 {
		t.Errorf("valid chunk should still load: got %d", got)
	}
}

func TestCoveredSeparatesMissingChunksFromWalls(t *testing.T) {
	dir := t.TempDir()
	testenv.WriteCostTile(t, dir, 32512, 32256, 7, testenv.CostTile(100, map[[2]int]uint8{{0, 0}: 255}))

	// The area reaches into the neighbouring chunk, which has no file at all.
	grid, err := LoadCostArea(dir, 7, image.Rect(32500, 32250, 32530, 32270))
	if err != nil {
		t.Fatal(err)
	}
	if !grid.Covered(32512, 32256) {
		t.Fatal("a tile from a decoded chunk reports as uncovered")
	}
	if grid.At(32512, 32256) != BlockedCost {
		t.Fatal("the blocked tile lost its cost")
	}
	if grid.Covered(32500, 32250) {
		t.Fatal("a tile with no chunk on disk reports as covered")
	}
	if grid.At(32500, 32250) != BlockedCost {
		t.Fatal("a missing tile must still be impassable for the search")
	}
}
