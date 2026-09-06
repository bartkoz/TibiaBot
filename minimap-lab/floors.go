package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sort"
)

type localAtlasEntry struct {
	atlas    *Atlas
	coverage image.Rectangle
	used     uint64
}

// Called under server.gate. Keep three bounded local atlases in addition to
// the one full atlas used for initial/global localization.
func (s *server) localAtlas(floor int, area image.Rectangle) (*Atlas, error) {
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
	atlas, err := loadAtlasArea(s.dir, floor, &coverage)
	if err != nil && !errors.Is(err, errNoMapData) {
		return nil, err
	}
	if _, exists := s.localAtlases[floor]; !exists && len(s.localAtlases) >= 3 {
		oldest := -1
		var used uint64
		for z, entry := range s.localAtlases {
			if oldest < 0 || entry.used < used {
				oldest = z
				used = entry.used
			}
		}
		delete(s.localAtlases, oldest)
	}
	s.localAtlases[floor] = localAtlasEntry{atlas, coverage, s.cacheClock}
	return atlas, nil
}

func localFootprint(im image.Image, o Options, near Position, radius int) image.Rectangle {
	// Round outward so partially visible enlarged cells and crop seams fit.
	return image.Rect(near.X-radius-(o.MarkerX+o.Zoom-1)/o.Zoom-2,
		near.Y-radius-(o.MarkerY+o.Zoom-1)/o.Zoom-2,
		near.X+radius+(im.Bounds().Dx()-o.MarkerX+o.Zoom-1)/o.Zoom+2,
		near.Y+radius+(im.Bounds().Dy()-o.MarkerY+o.Zoom-1)/o.Zoom+2)
}

// Try the selected floor first. If tracking there fails, compare BOTH adjacent
// floors against each other and the original candidate before accepting Z.
func (s *server) locateLocal(ctx context.Context, im image.Image, req matchRequest) (Result, *Atlas, error) {
	floorRadius := req.FloorRadius
	if floorRadius == 0 {
		floorRadius = 8
	}
	var results []Result
	var searched, unavailable []int
	atlases := make(map[int]*Atlas)
	searchFloor := func(z, radius int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		searched = append(searched, z)
		near := *req.Near
		near.Z = z
		atlas, err := s.localAtlas(z, localFootprint(im, req.Options, near, radius))
		if err != nil {
			return err
		}
		if atlas == nil {
			unavailable = append(unavailable, z)
			return nil
		}
		atlases[z] = atlas
		r, err := locateNear(ctx, atlas, im, req.Options, near, radius)
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
	result := Result{Mode: "local", Zoom: req.Zoom, Reason: "Brak dopasowania w pobliżu ostatniego XY na sprawdzonych piętrach."}
	if len(results) > 0 {
		result = results[0]
	}
	if result.Best != nil && len(results) > 1 && results[1].Best != nil && result.Best.Score-results[1].Best.Score <= req.MinGap {
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
	var atlas *Atlas
	if result.Best != nil {
		atlas = atlases[result.Best.Z]
	}
	if result.Found && result.Position.Z != req.Near.Z {
		result.FloorChanged = true
		result.Reason = fmt.Sprintf("Potwierdzono zmianę piętra Z=%d → %d w pobliżu poprzedniego XY.", req.Near.Z, result.Position.Z)
	}
	return result, atlas, nil
}
