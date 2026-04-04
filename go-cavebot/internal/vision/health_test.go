package vision

import (
	"testing"

	"gocv.io/x/gocv"
)

func fillBar(mat gocv.Mat, channel int, value uint8, startCol, endCol int) {
	for r := 0; r < mat.Rows(); r++ {
		for c := startCol; c < endCol; c++ {
			mat.SetUCharAt(r, c*3+channel, value)
		}
	}
}

func TestFullHealthBar(t *testing.T) {
	bar := gocv.NewMatWithSize(15, 200, gocv.MatTypeCV8UC3)
	defer bar.Close()
	fillBar(bar, 2, 200, 0, 200)
	pct := ReadBarPercentage(bar, 2, 100)
	if pct < 95 {
		t.Errorf("pct = %f, want >= 95", pct)
	}
}

func TestHalfHealthBar(t *testing.T) {
	bar := gocv.NewMatWithSize(15, 200, gocv.MatTypeCV8UC3)
	defer bar.Close()
	fillBar(bar, 2, 200, 0, 100)
	pct := ReadBarPercentage(bar, 2, 100)
	if pct < 45 || pct > 55 {
		t.Errorf("pct = %f, want 45-55", pct)
	}
}

func TestEmptyHealthBar(t *testing.T) {
	bar := gocv.NewMatWithSize(15, 200, gocv.MatTypeCV8UC3)
	defer bar.Close()
	pct := ReadBarPercentage(bar, 2, 100)
	if pct > 5 {
		t.Errorf("pct = %f, want <= 5", pct)
	}
}

func TestFullManaBar(t *testing.T) {
	bar := gocv.NewMatWithSize(15, 200, gocv.MatTypeCV8UC3)
	defer bar.Close()
	fillBar(bar, 0, 200, 0, 200)
	pct := ReadBarPercentage(bar, 0, 100)
	if pct < 95 {
		t.Errorf("pct = %f, want >= 95", pct)
	}
}

func TestQuarterManaBar(t *testing.T) {
	bar := gocv.NewMatWithSize(15, 200, gocv.MatTypeCV8UC3)
	defer bar.Close()
	fillBar(bar, 0, 200, 0, 50)
	pct := ReadBarPercentage(bar, 0, 100)
	if pct < 20 || pct > 30 {
		t.Errorf("pct = %f, want 20-30", pct)
	}
}
