package vision

import (
	"testing"

	"gocv.io/x/gocv"
)

func TestCropRegion(t *testing.T) {
	frame := gocv.NewMatWithSize(480, 640, gocv.MatTypeCV8UC3)
	defer frame.Close()
	// Set a pixel at col=100, row=50, red channel
	frame.SetUCharAt(50, 100*3+2, 255)

	cropped := CropRegion(frame, [4]int{90, 40, 20, 20})

	if cropped.Rows() != 20 || cropped.Cols() != 20 {
		t.Errorf("cropped size = %dx%d, want 20x20", cropped.Cols(), cropped.Rows())
	}
	// Original (100, 50) maps to cropped (10, 10)
	val := cropped.GetUCharAt(10, 10*3+2)
	if val != 255 {
		t.Errorf("pixel value = %d, want 255", val)
	}
}
