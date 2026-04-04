package vision

import (
	"testing"

	"gocv.io/x/gocv"
)

func fillRegion(mat gocv.Mat, value uint8) {
	for r := 0; r < mat.Rows(); r++ {
		for c := 0; c < mat.Cols(); c++ {
			mat.SetUCharAt(r, c*3+0, value)
			mat.SetUCharAt(r, c*3+1, value)
			mat.SetUCharAt(r, c*3+2, value)
		}
	}
}

func fillRect(mat gocv.Mat, r1, r2, c1, c2 int, value uint8) {
	for r := r1; r < r2; r++ {
		for c := c1; c < c2; c++ {
			mat.SetUCharAt(r, c*3+0, value)
			mat.SetUCharAt(r, c*3+1, value)
			mat.SetUCharAt(r, c*3+2, value)
		}
	}
}

func TestBattleListEmpty(t *testing.T) {
	region := gocv.NewMatWithSize(300, 180, gocv.MatTypeCV8UC3)
	defer region.Close()
	fillRegion(region, 20)
	if IsBattleListActive(region, 100, 0.02) {
		t.Error("expected inactive for dark region")
	}
}

func TestBattleListHasEntries(t *testing.T) {
	region := gocv.NewMatWithSize(300, 180, gocv.MatTypeCV8UC3)
	defer region.Close()
	fillRegion(region, 20)
	fillRect(region, 10, 25, 20, 160, 200)
	if !IsBattleListActive(region, 100, 0.02) {
		t.Error("expected active for bright entries")
	}
}

func TestLootWindowNotOpen(t *testing.T) {
	region := gocv.NewMatWithSize(200, 200, gocv.MatTypeCV8UC3)
	defer region.Close()
	if IsLootWindowOpen(region, 100, 0.15) {
		t.Error("expected loot window closed")
	}
}

func TestLootWindowOpen(t *testing.T) {
	region := gocv.NewMatWithSize(200, 200, gocv.MatTypeCV8UC3)
	defer region.Close()
	fillRegion(region, 20)
	fillRect(region, 20, 180, 20, 180, 180)
	if !IsLootWindowOpen(region, 100, 0.15) {
		t.Error("expected loot window open")
	}
}
