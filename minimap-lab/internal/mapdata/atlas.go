package mapdata

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

type Atlas struct {
	Image  *image.NRGBA
	Origin image.Point
	Floor  int
}

var chunkName = regexp.MustCompile(`^Minimap_Color_(\d+)_(\d+)_(\d+)\.png$`)
var ErrNoMapData = errors.New("brak danych mapy")

// ParseChunkName reads the coordinates out of a Minimap_Color file name,
// reporting whether the name is one at all. It keeps the file-name grammar -
// and the order of its capture groups - inside this package, so a caller that
// only wants to know which floors exist does not have to know either.
func ParseChunkName(name string) (x, y, z int, ok bool) {
	m := chunkName.FindStringSubmatch(name)
	if m == nil {
		return 0, 0, 0, false
	}
	// The pattern only matches digits, so the sizes are the only thing that
	// can fail here.
	x, errX := strconv.Atoi(m[1])
	y, errY := strconv.Atoi(m[2])
	z, errZ := strconv.Atoi(m[3])
	if errX != nil || errY != nil || errZ != nil {
		return 0, 0, 0, false
	}
	return x, y, z, true
}

// Unavailable chunks stay transparent: black terrain and missing data differ.
func LoadAtlas(dir string, floor int) (*Atlas, error) {
	return LoadAtlasArea(dir, floor, nil)
}

// Decode only the chunks overlapping a local search and its minimap footprint.
func LoadAtlasArea(dir string, floor int, area *image.Rectangle) (*Atlas, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type tile struct {
		path string
		x, y int
	}
	var tiles []tile
	var bounds image.Rectangle
	for _, e := range entries {
		m := chunkName.FindStringSubmatch(e.Name())
		if m == nil || e.IsDir() {
			continue
		}
		z, _ := strconv.Atoi(m[3])
		if z != floor {
			continue
		}
		x, err := strconv.Atoi(m[1])
		if err != nil || x > 65535 {
			return nil, fmt.Errorf("invalid chunk x: %s", e.Name())
		}
		y, err := strconv.Atoi(m[2])
		if err != nil || y > 65535 {
			return nil, fmt.Errorf("invalid chunk y: %s", e.Name())
		}
		if area != nil && !image.Rect(x, y, x+256, y+256).Overlaps(*area) {
			continue
		}
		tiles = append(tiles, tile{filepath.Join(dir, e.Name()), x, y})
		bounds = bounds.Union(image.Rect(x, y, x+256, y+256))
	}
	if len(tiles) == 0 {
		return nil, fmt.Errorf("%w: Minimap_Color_*_%d.png w %s", ErrNoMapData, floor, dir)
	}
	if int64(bounds.Dx())*int64(bounds.Dy()) > 32_000_000 {
		return nil, fmt.Errorf("atlas przekracza limit 32 mln pikseli; użyj map mniejszego obszaru")
	}
	out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for _, t := range tiles {
		f, err := os.Open(t.path)
		if err != nil {
			return nil, err
		}
		cfg, err := png.DecodeConfig(f)
		if err != nil || cfg.Width != 256 || cfg.Height != 256 {
			f.Close()
			return nil, fmt.Errorf("niepoprawny kafel 256×256: %s", t.path)
		}
		if _, err = f.Seek(0, 0); err != nil {
			f.Close()
			return nil, err
		}
		im, err := png.Decode(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		r := image.Rect(t.x, t.y, t.x+256, t.y+256).Sub(bounds.Min)
		draw.Draw(out, r, im, im.Bounds().Min, draw.Src)
	}
	return &Atlas{out, bounds.Min, floor}, nil
}
