package mapdata

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// Walking cost of one tile. 255 marks terrain that cannot be walked; the real
// tiles use 90-233 for walkable ground and never contain 0.
const BlockedCost = 255

// Chunk-aligned bounds can exceed the requested area, so the loader keeps its
// own ceiling instead of trusting the caller.
const maxCostTiles = 8 << 20

var costName = regexp.MustCompile(`^Minimap_WaypointCost_(\d+)_(\d+)_(\d+)\.png$`)

// CostGrid holds palette indices for one floor. bounds covers the decoded
// chunks, which are aligned to 256-tile boundaries; area is the smaller region
// a search may walk through.
type CostGrid struct {
	pix    []uint8
	bounds image.Rectangle
	area   image.Rectangle
	// covered lists the chunks actually decoded into pix. A missing chunk and a
	// real wall are both BlockedCost, which is right for the search and wrong
	// for the panel: an unvisited area would otherwise look like solid rock.
	covered []image.Rectangle
	// cheapest walkable tile in pix, in cost units. Zero is a legal cost, so
	// walkable says whether cheapest means anything at all.
	cheapest uint8
	walkable bool
}

// NewCostGrid builds a grid from per-tile costs the caller already has - one
// byte per tile, row-major over bounds - searchable across the whole
// rectangle. Narrow it afterwards with LimitTo.
//
// The buffer is copied. A CostGrid is shared between the floor cache and every
// LimitTo view of it, and the cheapest walkable cost is measured once here, so
// a caller that kept a reference and wrote to it later would both corrupt the
// cached floor and leave the search heuristic reading a minimum that no longer
// holds - the exact inadmissibility that makes A* return wrong routes.
//
// Every tile counts as covered: unlike the loader, which starts from blocked
// filler and marks only the chunks it decoded, nothing here is filler.
func NewCostGrid(pix []uint8, bounds image.Rectangle) *CostGrid {
	if want := bounds.Dx() * bounds.Dy(); len(pix) != want {
		panic(fmt.Sprintf("mapdata: %d kosztów dla obszaru %v, oczekiwano %d", len(pix), bounds, want))
	}
	g := &CostGrid{
		pix:     append([]uint8(nil), pix...),
		bounds:  bounds,
		area:    bounds,
		covered: []image.Rectangle{bounds},
	}
	g.measure()
	return g
}

// Covered reports whether the tile comes from a decoded chunk rather than from
// the blocked filler the loader starts with.
func (g *CostGrid) Covered(x, y int) bool {
	p := image.Pt(x, y)
	for _, r := range g.covered {
		if p.In(r) {
			return true
		}
	}
	return false
}

// At returns the walking cost at world coordinates. Everything outside the
// searchable area, including missing chunks, reads as blocked.
func (g *CostGrid) At(x, y int) uint8 {
	p := image.Pt(x, y)
	if !p.In(g.bounds) || !p.In(g.area) {
		return BlockedCost
	}
	return g.pix[(y-g.bounds.Min.Y)*g.bounds.Dx()+(x-g.bounds.Min.X)]
}

// Bounds is the rectangle the decoded chunks cover. A caller caching a grid
// needs it to tell whether the area it is about to search is already loaded.
func (g *CostGrid) Bounds() image.Rectangle { return g.bounds }

// Area is the part of the grid a search may walk through. It starts equal to
// Bounds and LimitTo narrows it, so a caller sizing an iteration budget by the
// number of reachable tiles wants this rather than Bounds.
func (g *CostGrid) Area() image.Rectangle { return g.area }

// CheapestWalkable reports the lowest cost any walkable tile in the grid has,
// and whether the grid holds a walkable tile at all. A search heuristic needs
// it to stay admissible: it is the floor on what one step can possibly cost.
func (g *CostGrid) CheapestWalkable() (uint8, bool) {
	return g.cheapest, g.walkable
}

// measure records the cheapest walkable tile, which bounds the per-step cost
// from below and keeps the search heuristic admissible.
func (g *CostGrid) measure() {
	g.cheapest, g.walkable = 0, false
	for _, v := range g.pix {
		if v != BlockedCost && (!g.walkable || v < g.cheapest) {
			g.cheapest, g.walkable = v, true
		}
	}
}

// LimitTo returns a view of the same pixels restricted to area. The caller can
// keep the full grid cached while searching only a slice of it.
func (g *CostGrid) LimitTo(area image.Rectangle) *CostGrid {
	view := *g
	view.area = area.Intersect(g.bounds)
	return &view
}

// LoadCostArea decodes the Minimap_WaypointCost chunks overlapping area.
// The palette of these PNGs is identity only up to 250, so the raw index is
// read instead of any color conversion.
func LoadCostArea(dir string, floor int, area image.Rectangle) (*CostGrid, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type tile struct {
		path string
		x, y int
	}
	var tiles []tile
	bounds := image.Rect(area.Min.X&^255, area.Min.Y&^255, (area.Max.X+255)&^255, (area.Max.Y+255)&^255)
	for _, e := range entries {
		m := costName.FindStringSubmatch(e.Name())
		if m == nil || e.IsDir() {
			continue
		}
		z, _ := strconv.Atoi(m[3])
		if z != floor {
			continue
		}
		// A stray or malformed file name is skipped, not fatal: one bad entry
		// in the maps directory must not take a whole floor down.
		x, err := strconv.Atoi(m[1])
		if err != nil || x > 65535 {
			continue
		}
		y, err := strconv.Atoi(m[2])
		if err != nil || y > 65535 {
			continue
		}
		if !image.Rect(x, y, x+256, y+256).Overlaps(bounds) {
			continue
		}
		tiles = append(tiles, tile{filepath.Join(dir, e.Name()), x, y})
	}
	if int64(bounds.Dx())*int64(bounds.Dy()) > maxCostTiles {
		return nil, fmt.Errorf("obszar kosztów przekracza limit %d kratek", maxCostTiles)
	}
	grid := &CostGrid{pix: make([]uint8, bounds.Dx()*bounds.Dy()), bounds: bounds, area: bounds}
	for i := range grid.pix {
		grid.pix[i] = BlockedCost
	}
	defer grid.measure()
	for _, t := range tiles {
		im, err := decodeCostTile(t.path)
		if err != nil {
			return nil, err
		}
		grid.covered = append(grid.covered, image.Rect(t.x, t.y, t.x+256, t.y+256))
		for y := 0; y < 256; y++ {
			wy := t.y + y
			if wy < bounds.Min.Y || wy >= bounds.Max.Y {
				continue
			}
			for x := 0; x < 256; x++ {
				wx := t.x + x
				if wx < bounds.Min.X || wx >= bounds.Max.X {
					continue
				}
				grid.pix[(wy-bounds.Min.Y)*bounds.Dx()+(wx-bounds.Min.X)] = im.Pix[y*im.Stride+x]
			}
		}
	}
	return grid, nil
}

func decodeCostTile(path string) (*image.Paletted, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	p, ok := im.(*image.Paletted)
	if !ok {
		return nil, fmt.Errorf("kafel kosztów nie jest obrazem z paletą: %s", path)
	}
	if p.Bounds().Dx() != 256 || p.Bounds().Dy() != 256 {
		return nil, fmt.Errorf("niepoprawny kafel 256×256: %s", path)
	}
	return p, nil
}
