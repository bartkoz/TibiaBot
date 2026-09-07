package locate

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sort"
	"sync"
	"time"

	"minimap-lab/internal/mapdata"
)

// Request is one localization query: the calibration, which floor to believe
// in, and - when tracking rather than acquiring - where to look.
type Request struct {
	Options
	Floor int  `json:"floor"`
	Demo  bool `json:"demo"`
	// Near switches the search from the whole floor to a window around a
	// previously confirmed position.
	Near   *mapdata.Position `json:"near,omitempty"`
	Radius int               `json:"radius,omitempty"`
	// The tags matter more than they look: the panel sends snake_case, and Go
	// matches field names case-insensitively but not across underscores. An
	// untagged AdjacentFloors would silently ignore adjacent_floors and switch
	// off automatic floor detection with nothing to show for it.
	AdjacentFloors bool `json:"adjacent_floors,omitempty"`
	FloorRadius    int  `json:"floor_radius,omitempty"`
}

// Validate holds the rules both the panel and the brain must obey, so a bad
// request is refused the same way whichever side built it.
func (r Request) Validate() error {
	if r.Floor < 0 || r.Floor > 15 {
		return errors.New("piętro musi mieścić się w zakresie 0–15")
	}
	if r.Near == nil {
		return nil
	}
	if r.Near.Z < 0 || r.Near.Z > 15 || abs(r.Near.Z-r.Floor) > 1 ||
		r.Near.X < 0 || r.Near.X > 65535 || r.Near.Y < 0 || r.Near.Y > 65535 ||
		r.Zoom < 1 || r.Zoom > 8 || r.Radius < 1 || r.Radius > 64 ||
		r.FloorRadius < 0 || r.FloorRadius > 32 {
		return errors.New("nieprawidłowy obszar lokalny: wymagane Z lub Z±1, skala 1–8, promień ruchu 1–64 i promień przejścia 1–32 (0 = 8)")
	}
	return nil
}

type localAtlasEntry struct {
	atlas    *mapdata.Atlas
	coverage image.Rectangle
	used     uint64
}

// Service owns the decoded map data localization needs: one full atlas for the
// floor being acquired, plus a few bounded ones for local tracking. The mutex
// makes it safe to share; in practice the brain loop is the only caller, and
// the panel's own handler goes away with the intent protocol.
type Service struct {
	mu           sync.Mutex
	dir          string
	cached       *mapdata.Atlas
	localAtlases map[int]localAtlasEntry
	cacheClock   uint64
}

func NewService(dir string) *Service { return &Service{dir: dir} }

// Locate answers one query and returns the atlas the answer came from, which
// the caller needs to cut a preview out of.
func (s *Service) Locate(ctx context.Context, im image.Image, req Request) (Result, *mapdata.Atlas, error) {
	if err := req.Validate(); err != nil {
		return Result{}, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	matchStarted := time.Now()
	var atlas *mapdata.Atlas
	var result Result
	var err error
	switch {
	case req.Demo:
		atlas = mapdata.DemoAtlas()
	case req.Near != nil:
		result, atlas, err = s.locateLocalLocked(ctx, im, req)
	default:
		if s.cached == nil || s.cached.Floor != req.Floor {
			s.cached = nil
			s.cached, err = mapdata.LoadAtlas(s.dir, req.Floor)
		}
		atlas = s.cached
	}
	if err != nil {
		return Result{}, nil, err
	}
	// The demo atlas is synthetic, so it goes through whichever search the
	// request asked for rather than having its own path.
	if req.Near != nil && req.Demo {
		result, err = Near(ctx, atlas, im, req.Options, *req.Near, req.Radius)
	} else if req.Near == nil {
		result, err = WithScale(ctx, atlas, im, req.Options)
	}
	if err != nil {
		return Result{}, nil, err
	}
	result.MatchMS = float64(time.Since(matchStarted).Microseconds()) / 1000
	return result, atlas, nil
}

// localAtlasLocked keeps three bounded local atlases in addition to the one
// full atlas used for initial and global localization.
func (s *Service) localAtlasLocked(floor int, area image.Rectangle) (*mapdata.Atlas, error) {
	if s.cached != nil && s.cached.Floor == floor {
		return s.cached, nil
	}
	if s.localAtlases == nil {
		s.localAtlases = make(map[int]localAtlasEntry)
	}
	s.cacheClock++
	if entry, ok := s.localAtlases[floor]; ok && area.In(entry.coverage) {
		entry.used = s.cacheClock
		s.localAtlases[floor] = entry
		return entry.atlas, nil
	}
	// Align coverage with tile boundaries, including known missing chunks.
	coverage := image.Rect(area.Min.X&^255, area.Min.Y&^255, (area.Max.X+255)&^255, (area.Max.Y+255)&^255)
	atlas, err := mapdata.LoadAtlasArea(s.dir, floor, &coverage)
	if err != nil && !errors.Is(err, mapdata.ErrNoMapData) {
		return nil, err
	}
	if _, exists := s.localAtlases[floor]; !exists && len(s.localAtlases) >= 3 {
		oldest := -1
		var used uint64
		for z, entry := range s.localAtlases {
			if oldest < 0 || entry.used < used {
				oldest, used = z, entry.used
			}
		}
		delete(s.localAtlases, oldest)
	}
	s.localAtlases[floor] = localAtlasEntry{atlas, coverage, s.cacheClock}
	return atlas, nil
}

func localFootprint(im image.Image, o Options, near mapdata.Position, radius int) image.Rectangle {
	// Round outward so partially visible enlarged cells and crop seams fit.
	return image.Rect(near.X-radius-(o.MarkerX+o.Zoom-1)/o.Zoom-2,
		near.Y-radius-(o.MarkerY+o.Zoom-1)/o.Zoom-2,
		near.X+radius+(im.Bounds().Dx()-o.MarkerX+o.Zoom-1)/o.Zoom+2,
		near.Y+radius+(im.Bounds().Dy()-o.MarkerY+o.Zoom-1)/o.Zoom+2)
}

// locateLocalLocked tries the selected floor first. If tracking there fails, it
// compares BOTH adjacent floors against each other and the original candidate
// before accepting Z.
func (s *Service) locateLocalLocked(ctx context.Context, im image.Image, req Request) (Result, *mapdata.Atlas, error) {
	floorRadius := req.FloorRadius
	if floorRadius == 0 {
		floorRadius = 8
	}
	var results []Result
	var searched, unavailable []int
	atlases := make(map[int]*mapdata.Atlas)
	searchFloor := func(z, radius int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		searched = append(searched, z)
		near := *req.Near
		near.Z = z
		atlas, err := s.localAtlasLocked(z, localFootprint(im, req.Options, near, radius))
		if err != nil {
			return err
		}
		if atlas == nil {
			unavailable = append(unavailable, z)
			return nil
		}
		atlases[z] = atlas
		r, err := Near(ctx, atlas, im, req.Options, near, radius)
		if err != nil {
			return err
		}
		results = append(results, r)
		return nil
	}
	radius := req.Radius
	if req.Floor != req.Near.Z {
		radius = floorRadius
	}
	if err := searchFloor(req.Floor, radius); err != nil {
		return Result{}, nil, err
	}
	primaryFound := len(results) > 0 && results[0].Found
	if !primaryFound && req.AdjacentFloors && req.Floor == req.Near.Z {
		for _, z := range []int{req.Near.Z - 1, req.Near.Z + 1} {
			if z >= 0 && z <= 15 {
				if err := searchFloor(z, floorRadius); err != nil {
					return Result{}, nil, err
				}
			}
		}
	}
	positions := 0
	for _, r := range results {
		positions += r.SearchPositions
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Best == nil {
			return false
		}
		if results[j].Best == nil {
			return true
		}
		return results[i].Best.Score > results[j].Best.Score
	})
	result := Result{Mode: "local", Zoom: req.Zoom,
		Reason: "Brak dopasowania w pobliżu ostatniego XY na sprawdzonych piętrach."}
	if len(results) > 0 {
		result = results[0]
	}
	if result.Best != nil && len(results) > 1 && results[1].Best != nil &&
		result.Best.Score-results[1].Best.Score <= req.MinGap {
		result.Found = false
		result.Position = nil
		if result.Competitor == nil || results[1].Best.Score > result.Competitor.Score {
			result.Competitor = results[1].Best
		}
		result.Reason = "Niejednoznaczne piętro: podobne dopasowania na różnych Z. Pozycja pozostaje nieznana."
	}
	result.SearchPositions = positions
	result.SearchedFloors = searched
	result.UnavailableFloors = unavailable
	var atlas *mapdata.Atlas
	if result.Best != nil {
		atlas = atlases[result.Best.Z]
	}
	if result.Found && result.Position.Z != req.Near.Z {
		result.FloorChanged = true
		result.Reason = fmt.Sprintf("Potwierdzono zmianę piętra Z=%d → %d w pobliżu poprzedniego XY.", req.Near.Z, result.Position.Z)
	}
	return result, atlas, nil
}
