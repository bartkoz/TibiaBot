package vision

import (
	"image"

	"gocv.io/x/gocv"
)

// FrameCapture wraps GoCV VideoCapture for reading OBS Virtual Camera frames.
type FrameCapture struct {
	cameraIndex int
	cap         *gocv.VideoCapture
}

func NewFrameCapture(cameraIndex int) *FrameCapture {
	return &FrameCapture{cameraIndex: cameraIndex}
}

func (fc *FrameCapture) Open() bool {
	cap, err := gocv.OpenVideoCapture(fc.cameraIndex)
	if err != nil {
		return false
	}
	fc.cap = cap
	return fc.cap.IsOpened()
}

func (fc *FrameCapture) Read(dst *gocv.Mat) bool {
	if fc.cap == nil {
		return false
	}
	return fc.cap.Read(dst)
}

func (fc *FrameCapture) Close() {
	if fc.cap != nil {
		fc.cap.Close()
		fc.cap = nil
	}
}

// CropRegion extracts a sub-region from a frame. region is [x, y, w, h].
func CropRegion(frame gocv.Mat, region [4]int) gocv.Mat {
	x, y, w, h := region[0], region[1], region[2], region[3]
	rect := image.Rect(x, y, x+w, y+h)
	return frame.Region(rect)
}
