package vision

import "gocv.io/x/gocv"

// ReadBarPercentage reads the middle row of a bar image and returns
// the percentage of pixels in the given channel that exceed the threshold.
// channel: 0=blue, 1=green, 2=red (BGR order).
func ReadBarPercentage(barImage gocv.Mat, channel int, threshold uint8) float64 {
	if barImage.Empty() {
		return 0.0
	}
	midRow := barImage.Rows() / 2
	cols := barImage.Cols()
	if cols == 0 {
		return 0.0
	}
	filled := 0
	for c := 0; c < cols; c++ {
		val := barImage.GetUCharAt(midRow, c*3+channel)
		if val >= threshold {
			filled++
		}
	}
	return (float64(filled) / float64(cols)) * 100.0
}
