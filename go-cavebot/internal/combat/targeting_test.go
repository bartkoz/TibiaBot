package combat

import (
	"testing"

	"gocv.io/x/gocv"
)

func TestNoTargetWhenBattleListEmpty(t *testing.T) {
	ts := &TargetingSystem{}
	region := gocv.NewMatWithSize(300, 180, gocv.MatTypeCV8UC3)
	defer region.Close()
	for r := 0; r < 300; r++ {
		for c := 0; c < 180; c++ {
			region.SetUCharAt(r, c*3+0, 20)
			region.SetUCharAt(r, c*3+1, 20)
			region.SetUCharAt(r, c*3+2, 20)
		}
	}
	if ts.HasTarget(region) {
		t.Error("expected no target for dark region")
	}
}

func TestHasTargetWhenBattleListActive(t *testing.T) {
	ts := &TargetingSystem{}
	region := gocv.NewMatWithSize(300, 180, gocv.MatTypeCV8UC3)
	defer region.Close()
	for r := 0; r < 300; r++ {
		for c := 0; c < 180; c++ {
			region.SetUCharAt(r, c*3+0, 20)
			region.SetUCharAt(r, c*3+1, 20)
			region.SetUCharAt(r, c*3+2, 20)
		}
	}
	for r := 10; r < 25; r++ {
		for c := 20; c < 160; c++ {
			region.SetUCharAt(r, c*3+0, 200)
			region.SetUCharAt(r, c*3+1, 200)
			region.SetUCharAt(r, c*3+2, 200)
		}
	}
	if !ts.HasTarget(region) {
		t.Error("expected target for bright region")
	}
}
