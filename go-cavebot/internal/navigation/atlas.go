package navigation

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

const ChunkSize = 256

type ChunkKey struct {
	X, Y, Z int
}

type Bounds struct {
	MinX int `json:"min_x"`
	MinY int `json:"min_y"`
	MaxX int `json:"max_x"`
	MaxY int `json:"max_y"`
}

var (
	colorRe = regexp.MustCompile(`Minimap_Color_(\d+)_(\d+)_(\d+)\.png`)
	costRe  = regexp.MustCompile(`Minimap_WaypointCost_(\d+)_(\d+)_(\d+)\.png`)
)

type Atlas struct {
	dir         string
	ColorChunks map[ChunkKey]*image.RGBA
	CostChunks  map[ChunkKey]*image.Gray
	ZLevels     map[int]bool
}

func NewAtlas(dir string) *Atlas {
	return &Atlas{
		dir:         dir,
		ColorChunks: make(map[ChunkKey]*image.RGBA),
		CostChunks:  make(map[ChunkKey]*image.Gray),
		ZLevels:     make(map[int]bool),
	}
}

func (a *Atlas) Load() error {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()

		if m := colorRe.FindStringSubmatch(name); m != nil {
			cx, _ := strconv.Atoi(m[1])
			cy, _ := strconv.Atoi(m[2])
			cz, _ := strconv.Atoi(m[3])
			key := ChunkKey{cx, cy, cz}
			img, err := loadPNG(filepath.Join(a.dir, name))
			if err != nil {
				continue
			}
			a.ColorChunks[key] = toRGBA(img)
			a.ZLevels[cz] = true
			continue
		}

		if m := costRe.FindStringSubmatch(name); m != nil {
			cx, _ := strconv.Atoi(m[1])
			cy, _ := strconv.Atoi(m[2])
			cz, _ := strconv.Atoi(m[3])
			key := ChunkKey{cx, cy, cz}
			img, err := loadPNG(filepath.Join(a.dir, name))
			if err != nil {
				continue
			}
			a.CostChunks[key] = toGray(img)
			a.ZLevels[cz] = true
		}
	}
	return nil
}

func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func toRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	return rgba
}

func toGray(img image.Image) *image.Gray {
	if gray, ok := img.(*image.Gray); ok {
		return gray
	}
	bounds := img.Bounds()
	gray := image.NewGray(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			// Use luminance for grayscale conversion
			lum := (r*299 + g*587 + b*114) / 1000
			gray.SetGray(x, y, color.Gray{Y: uint8(lum >> 8)})
		}
	}
	return gray
}

func (a *Atlas) GetColorTile(chunkX, chunkY, z int) (*image.RGBA, bool) {
	tile, ok := a.ColorChunks[ChunkKey{chunkX, chunkY, z}]
	return tile, ok
}

func (a *Atlas) GetCostAt(worldX, worldY, z int) int {
	chunkX := (worldX / ChunkSize) * ChunkSize
	chunkY := (worldY / ChunkSize) * ChunkSize
	tile, ok := a.CostChunks[ChunkKey{chunkX, chunkY, z}]
	if !ok {
		return 255
	}
	px := worldX - chunkX
	py := worldY - chunkY
	if px < 0 || px >= ChunkSize || py < 0 || py >= ChunkSize {
		return 255
	}
	return int(tile.GrayAt(px, py).Y)
}

func (a *Atlas) ChunkKeysForZ(z int) []ChunkKey {
	var keys []ChunkKey
	for k := range a.ColorChunks {
		if k.Z == z {
			keys = append(keys, k)
		}
	}
	return keys
}

func (a *Atlas) WorldBounds(z int) Bounds {
	keys := a.ChunkKeysForZ(z)
	if len(keys) == 0 {
		return Bounds{}
	}
	minX, minY := keys[0].X, keys[0].Y
	maxX, maxY := keys[0].X, keys[0].Y
	for _, k := range keys[1:] {
		if k.X < minX {
			minX = k.X
		}
		if k.Y < minY {
			minY = k.Y
		}
		if k.X > maxX {
			maxX = k.X
		}
		if k.Y > maxY {
			maxY = k.Y
		}
	}
	return Bounds{
		MinX: minX,
		MinY: minY,
		MaxX: maxX + ChunkSize,
		MaxY: maxY + ChunkSize,
	}
}
