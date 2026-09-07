// Package frame parses the binary body the panel posts to /api/frame. The
// panel is the only source of pictures in the whole system - Go has no screen
// capture of its own - so this is the hot path: at the tracking cadence it
// runs many times a second and must not copy the pixels it is handed.
package frame

import (
	"encoding/binary"
	"fmt"
	"image"
)

const (
	Magic         = "MLF1"
	FormatVersion = 1
	// MaxBody bounds one request. At the calibration in internal/brain's own
	// tests, the viewport crop is roughly 124 KB and the battle list roughly
	// 141 KB; at a 32 px/tile client with a decision radius of 8 tiles the
	// viewport crop alone is roughly 1.5 MB. Four megabytes is real headroom
	// for a larger client, not slack four orders of magnitude wide.
	MaxBody = 4 << 20
	// HeaderSize and RegionHeader are the fixed layout described in the spec.
	HeaderSize   = 36
	RegionHeader = 12
	// MaxRegions is deliberately larger than the five ids in use, so a future
	// region does not need a protocol bump - only a new constant.
	MaxRegions    = 8
	MaxRegionSide = 1024
)

type RegionID uint8

const (
	RegionMinimap  RegionID = 1
	RegionHP       RegionID = 2
	RegionMana     RegionID = 3
	RegionViewport RegionID = 4
	RegionBattle   RegionID = 5
)

func knownRegion(id RegionID) bool {
	return id == RegionMinimap || id == RegionHP || id == RegionMana ||
		id == RegionViewport || id == RegionBattle
}

type Region struct {
	ID   RegionID
	W, H int
	// Pix is non-premultiplied RGBA, straight out of the canvas.
	Pix []byte
}

type Frame struct {
	// Session ties the frame to one getDisplayMedia stream. A reload leaves
	// the previous stream's frames in flight; they must not be believed.
	Session uint64
	Seq     uint64
	// VideoTimeUS is video.currentTime. The same value twice means the same
	// picture twice - one observation, not two. Network traffic is no proof
	// that the picture moved.
	VideoTimeUS uint64
	// AgeMS is how long the panel took between cutting the pixels and sending
	// them. The server adds its own elapsed time on top.
	AgeMS   uint32
	Regions map[RegionID]Region
}

func Parse(data []byte) (Frame, error) {
	if len(data) < HeaderSize {
		return Frame{}, fmt.Errorf("ciało krótsze niż nagłówek klatki (%d < %d)", len(data), HeaderSize)
	}
	if string(data[0:4]) != Magic {
		return Frame{}, fmt.Errorf("nieznany format klatki")
	}
	if data[4] != FormatVersion {
		return Frame{}, fmt.Errorf("nieobsługiwana wersja formatu klatki: %d", data[4])
	}
	count := int(data[5])
	if count > MaxRegions {
		return Frame{}, fmt.Errorf("zbyt wiele regionów: %d", count)
	}
	if flags := binary.LittleEndian.Uint16(data[6:]); flags != 0 {
		return Frame{}, fmt.Errorf("pole flag jest zarezerwowane i musi być zerowe")
	}
	f := Frame{
		Session:     binary.LittleEndian.Uint64(data[8:]),
		Seq:         binary.LittleEndian.Uint64(data[16:]),
		VideoTimeUS: binary.LittleEndian.Uint64(data[24:]),
		AgeMS:       binary.LittleEndian.Uint32(data[32:]),
		Regions:     make(map[RegionID]Region, count),
	}
	headersEnd := HeaderSize + RegionHeader*count
	if len(data) < headersEnd {
		return Frame{}, fmt.Errorf("ciało nie mieści nagłówków %d regionów", count)
	}
	type pending struct {
		region Region
		length int
	}
	order := make([]pending, 0, count)
	total := 0
	for i := 0; i < count; i++ {
		h := data[HeaderSize+RegionHeader*i:]
		id := RegionID(h[0])
		if !knownRegion(id) {
			return Frame{}, fmt.Errorf("nieznany region: %d", id)
		}
		if _, seen := f.Regions[id]; seen {
			return Frame{}, fmt.Errorf("region %d powtórzony w jednej klatce", id)
		}
		if h[1] != 0 {
			return Frame{}, fmt.Errorf("nieznany format pikseli regionu %d: %d", id, h[1])
		}
		w := int(binary.LittleEndian.Uint16(h[4:]))
		height := int(binary.LittleEndian.Uint16(h[6:]))
		if w < 1 || height < 1 || w > MaxRegionSide || height > MaxRegionSide {
			return Frame{}, fmt.Errorf("region %d ma niedozwolone wymiary %dx%d", id, w, height)
		}
		length := int(binary.LittleEndian.Uint32(h[8:]))
		// A declared length that disagrees with the dimensions is what a
		// truncated or mismatched upload looks like. Believing it would hand
		// the matcher a buffer it reads past the end of.
		if length != w*height*4 {
			return Frame{}, fmt.Errorf("region %d deklaruje %d bajtów zamiast %d", id, length, w*height*4)
		}
		total += length
		// Reserve the slot now so a duplicate later in the same body is caught.
		f.Regions[id] = Region{ID: id, W: w, H: height}
		order = append(order, pending{region: Region{ID: id, W: w, H: height}, length: length})
	}
	payload := data[headersEnd:]
	if len(payload) != total {
		return Frame{}, fmt.Errorf("ładunek ma %d bajtów, zadeklarowano %d", len(payload), total)
	}
	at := 0
	for _, p := range order {
		r := p.region
		// Slicing, not copying: the caller wraps this in an image and reads it
		// once. The buffer belongs to the request and outlives neither.
		r.Pix = payload[at : at+p.length]
		f.Regions[r.ID] = r
		at += p.length
	}
	return f, nil
}

// Image wraps one region's pixels as an NRGBA without copying them. Canvas
// getImageData produces non-premultiplied RGBA, which is precisely NRGBA's
// layout, so no conversion is needed either.
func (f Frame) Image(id RegionID) (*image.NRGBA, bool) {
	r, ok := f.Regions[id]
	if !ok || r.Pix == nil {
		return nil, false
	}
	return &image.NRGBA{
		Pix:    r.Pix,
		Stride: r.W * 4,
		Rect:   image.Rect(0, 0, r.W, r.H),
	}, true
}
