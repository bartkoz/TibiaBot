package vision

import "gocv.io/x/gocv"

// IsBattleListActive checks brightness ratio to detect enemies in the battle list.
func IsBattleListActive(region gocv.Mat, brightnessThreshold uint8, minBrightRatio float64) bool {
	return brightRatio(region, brightnessThreshold) > minBrightRatio
}

// IsLootWindowOpen checks brightness ratio to detect a loot window.
func IsLootWindowOpen(region gocv.Mat, brightnessThreshold uint8, minBrightRatio float64) bool {
	return brightRatio(region, brightnessThreshold) > minBrightRatio
}

func brightRatio(region gocv.Mat, threshold uint8) float64 {
	if region.Empty() {
		return 0
	}
	rows := region.Rows()
	cols := region.Cols()
	total := rows * cols
	if total == 0 {
		return 0
	}
	bright := 0
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			b := int(region.GetUCharAt(r, c*3+0))
			g := int(region.GetUCharAt(r, c*3+1))
			rv := int(region.GetUCharAt(r, c*3+2))
			avg := (b + g + rv) / 3
			if avg > int(threshold) {
				bright++
			}
		}
	}
	return float64(bright) / float64(total)
}
