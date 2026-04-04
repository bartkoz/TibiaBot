package combat

import (
	"github.com/anthropics/tibiabot/internal/vision"
	"gocv.io/x/gocv"
)

type TargetingSystem struct{}

func (ts *TargetingSystem) HasTarget(battleListRegion gocv.Mat) bool {
	return vision.IsBattleListActive(battleListRegion, 100, 0.02)
}
