package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"sort"
)

type Options struct {
	Zoom       int     `json:"zoom"`
	MarkerX    int     `json:"marker_x"`
	MarkerY    int     `json:"marker_y"`
	MaskRadius int     `json:"mask_radius"`
	MinScore   float64 `json:"min_score"`
	MinGap     float64 `json:"min_gap"`
}

type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
	Z int `json:"z"`
}
type Candidate struct {
	Position
	Score float64 `json:"score"`
}
type Result struct {
	Found             bool         `json:"found"`
	Reason            string       `json:"reason"`
	Position          *Position    `json:"position"`
	Best              *Candidate   `json:"best,omitempty"`
	Competitor        *Candidate   `json:"competitor,omitempty"`
	Samples           int          `json:"samples"`
	ElapsedMS         int64        `json:"elapsed_ms"`
	Zoom              int          `json:"zoom"`
	ScaleScores       []ScaleScore `json:"scale_scores,omitempty"`
	Mode              string       `json:"mode"`
	SearchPositions   int          `json:"search_positions"`
	MatchMS           float64      `json:"match_ms"`
	SearchedFloors    []int        `json:"searched_floors,omitempty"`
	UnavailableFloors []int        `json:"unavailable_floors,omitempty"`
	FloorChanged      bool         `json:"floor_changed"`
}
type ScaleScore struct {
	Zoom  int     `json:"zoom"`
	Score float64 `json:"score"`
}
type sample struct {
	dx, dy  int
	r, g, b int
	edge    bool
}

func makeSamples(im image.Image, o Options) ([]sample, error) {
	w, h := im.Bounds().Dx(), im.Bounds().Dy()
	if o.Zoom < 1 || o.Zoom > 8 || w < 8 || h < 8 || w > 1024 || h > 1024 {
		return nil, fmt.Errorf("wycinek: 8–1024 px, skala: całkowita liczba 1–8 px/kratkę")
	}
	if o.MarkerX < 0 || o.MarkerX >= w || o.MarkerY < 0 || o.MarkerY >= h || o.MaskRadius < 0 || o.MaskRadius > 64 {
		return nil, fmt.Errorf("znacznik musi leżeć wewnątrz minimapy, promień maski: 0–64 px")
	}
	if math.IsNaN(o.MinScore) || math.IsNaN(o.MinGap) || o.MinScore < .5 || o.MinScore > 1 || o.MinGap <= 0 || o.MinGap > .2 {
		return nil, fmt.Errorf("próg wyniku: 0.5–1; odstęp od konkurenta: >0 do 0.2")
	}
	var samples []sample
	visible := 0
	// Sample cell centers relative to the manually selected player marker.
	// This also handles a crop beginning halfway through an enlarged cell.
	for y := o.MarkerY % o.Zoom; y < h; y += o.Zoom {
		for x := o.MarkerX % o.Zoom; x < w; x += o.Zoom {
			if abs(x-o.MarkerX) <= o.MaskRadius && abs(y-o.MarkerY) <= o.MaskRadius {
				continue
			}
			c := color.NRGBAModel.Convert(im.At(im.Bounds().Min.X+x, im.Bounds().Min.Y+y)).(color.NRGBA)
			// Black borders around explored floor encode the shape of a cave.
			// Discarding them makes a one-color cave indistinguishable from a flat field.
			if c.A < 250 {
				continue
			}
			if c.R >= 8 || c.G >= 8 || c.B >= 8 {
				visible++
			}
			edge := false
			for _, d := range []image.Point{{o.Zoom, 0}, {-o.Zoom, 0}, {0, o.Zoom}, {0, -o.Zoom}} {
				xx, yy := x+d.X, y+d.Y
				if xx < 0 || yy < 0 || xx >= w || yy >= h || (abs(xx-o.MarkerX) <= o.MaskRadius && abs(yy-o.MarkerY) <= o.MaskRadius) {
					continue
				}
				n := color.NRGBAModel.Convert(im.At(im.Bounds().Min.X+xx, im.Bounds().Min.Y+yy)).(color.NRGBA)
				if n.A >= 250 && abs(int(c.R)-int(n.R))+abs(int(c.G)-int(n.G))+abs(int(c.B)-int(n.B)) > 60 {
					edge = true
					break
				}
			}
			samples = append(samples, sample{(x - o.MarkerX) / o.Zoom, (y - o.MarkerY) / o.Zoom, int(c.R), int(c.G), int(c.B), edge})
		}
	}
	if visible < 64 {
		return nil, fmt.Errorf("za mało widocznego terenu: %d kratek, wymagane 64", visible)
	}
	// Deterministic shuffle distributes early rejection across the whole image.
	rand.New(rand.NewSource(1)).Shuffle(len(samples), func(i, j int) { samples[i], samples[j] = samples[j], samples[i] })
	// Spend the limited sample budget on boundaries first. Large uniform rooms
	// otherwise drown out the evidence separating neighboring tile coordinates.
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].edge && !samples[j].edge })
	if len(samples) > 1024 {
		samples = samples[:1024]
	}
	var sums, squares [3]float64
	for _, s := range samples {
		for k, v := range []int{s.r, s.g, s.b} {
			sums[k] += float64(v)
			squares[k] += float64(v * v)
		}
	}
	variance := 0.0
	for k := range sums {
		mean := sums[k] / float64(len(samples))
		variance += squares[k]/float64(len(samples)) - mean*mean
	}
	if variance < 100 {
		return nil, fmt.Errorf("teren zbyt jednolity do wiarygodnej lokalizacji")
	}
	return samples, nil
}

// Automatic calibration tries the usual integer scales, stopping at the first
// unambiguous match above the user's threshold. The UI locks that scale until
// the source is changed or the user asks for automatic calibration again.
func locateWithScale(ctx context.Context, atlas *Atlas, im image.Image, o Options) (Result, error) {
	if o.Zoom != 0 {
		return locate(ctx, atlas, im, o)
	}
	var best Result
	var scores []ScaleScore
	var lastErr error
	valid := false
	for _, zoom := range []int{1, 2, 3, 4} {
		o.Zoom = zoom
		result, err := locate(ctx, atlas, im, o)
		if err != nil {
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			lastErr = err
			continue
		}
		valid = true
		if result.Best != nil {
			scores = append(scores, ScaleScore{zoom, result.Best.Score})
			if best.Best == nil || result.Best.Score > best.Best.Score {
				best = result
			}
		}
		if result.Found {
			result.ScaleScores = scores
			return result, nil
		}
	}
	if !valid {
		return Result{}, lastErr
	}
	best.ScaleScores = scores
	if best.Reason == "" {
		best.Reason = "Nie znaleziono pasującej skali 1–4 px/kratkę. Sprawdź piętro i wycinek."
	}
	return best, nil
}

// locate uses bounded RGB absolute error, not a calibrated probability.
// Pass two searches for a distinct competing location within MinGap of best.
func locate(ctx context.Context, atlas *Atlas, im image.Image, o Options) (Result, error) {
	return locateIn(ctx, atlas, im, o, nil)
}

// near and radius constrain PLAYER coordinates, not the top-left of the crop.
func locateNear(ctx context.Context, atlas *Atlas, im image.Image, o Options, near Position, radius int) (Result, error) {
	if radius < 1 || radius > 64 || near.X < 0 || near.X > 65535 || near.Y < 0 || near.Y > 65535 || near.Z != atlas.Floor || o.Zoom == 0 {
		return Result{}, fmt.Errorf("lokalne śledzenie wymaga znanej skali, zgodnego piętra, XYZ 0–65535 i promienia 1–64")
	}
	area := image.Rect(near.X-radius, near.Y-radius, near.X+radius+1, near.Y+radius+1)
	result, err := locateIn(ctx, atlas, im, o, &area)
	if err == nil && result.Found && (abs(result.Position.X-near.X) == radius || abs(result.Position.Y-near.Y) == radius) {
		result.Found = false
		result.Position = nil
		result.Reason = "Kandydat na granicy obszaru śledzenia. Potrzebny szerszy odczyt."
	}
	return result, err
}

func locateIn(ctx context.Context, atlas *Atlas, im image.Image, o Options, worldArea *image.Rectangle) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	samples, err := makeSamples(im, o)
	if err != nil {
		return Result{}, err
	}
	result := Result{Samples: len(samples), Zoom: o.Zoom, Reason: "Brak dopasowania powyżej progu. Sprawdź piętro, skalę i wycinek."}
	result.Mode = "global"
	b := atlas.Image.Bounds()
	minDX, maxDX, minDY, maxDY := 0, 0, 0, 0
	for _, s := range samples {
		minDX = min(minDX, s.dx)
		maxDX = max(maxDX, s.dx)
		minDY = min(minDY, s.dy)
		maxDY = max(maxDY, s.dy)
	}
	search := image.Rectangle{Min: image.Pt(-minDX, -minDY), Max: image.Pt(b.Dx()-maxDX, b.Dy()-maxDY)}
	if worldArea != nil {
		search = search.Intersect(worldArea.Sub(atlas.Origin))
		result.Mode = "local"
	}
	if search.Empty() {
		return result, nil
	}
	result.SearchPositions = search.Dx() * search.Dy()
	denom := float64(len(samples) * 3 * 255)
	maxLoss := int(math.Floor((1-o.MinScore)*denom + 1e-8))
	offsets := make([]int, len(samples))
	for i, s := range samples {
		offsets[i] = s.dy*atlas.Image.Stride + s.dx*4
	}
	pix := atlas.Image.Pix
	bestPoint := image.Pt(-1, -1)
	bestLoss := maxLoss + 1
	scan := func(limit int, exclude *image.Point) (image.Point, int, error) {
		point := image.Pt(-1, -1)
		lossBest := limit + 1
		for y := search.Min.Y; y < search.Max.Y; y++ {
			if y%8 == 0 {
				if err := ctx.Err(); err != nil {
					return point, lossBest, err
				}
			}
			for x := search.Min.X; x < search.Max.X; x++ {
				// Even a one-cell alternative matters when reporting tile coordinates.
				if exclude != nil && x == exclude.X && y == exclude.Y {
					continue
				}
				base := y*atlas.Image.Stride + x*4
				if pix[base+3] != 255 {
					continue
				}
				loss := 0
				for i, s := range samples {
					p := base + offsets[i]
					if pix[p+3] != 255 {
						loss = lossBest
						break
					}
					loss += abs(int(pix[p])-s.r) + abs(int(pix[p+1])-s.g) + abs(int(pix[p+2])-s.b)
					if loss >= lossBest {
						break
					}
				}
				if loss < lossBest {
					lossBest = loss
					point = image.Pt(x, y)
				}
			}
		}
		return point, lossBest, nil
	}
	// Retain the strongest candidate even below acceptance threshold for diagnosis.
	bestPoint, bestLoss, err = scan(int(denom), nil)
	if err != nil {
		return result, err
	}
	if bestPoint.X < 0 {
		return result, nil
	}
	candidate := func(p image.Point, loss int) *Candidate {
		return &Candidate{Position{atlas.Origin.X + p.X, atlas.Origin.Y + p.Y, atlas.Floor}, 1 - float64(loss)/denom}
	}
	result.Best = candidate(bestPoint, bestLoss)
	if bestLoss > maxLoss {
		result.Reason = fmt.Sprintf("Najlepszy wynik %.2f%% jest poniżej progu %.2f%%. Sprawdź skalę, piętro i wycinek.", result.Best.Score*100, o.MinScore*100)
		return result, nil
	}
	other, loss, err := scan(bestLoss+int(math.Ceil(o.MinGap*denom)), &bestPoint)
	if err != nil {
		return result, err
	}
	if other.X >= 0 {
		result.Competitor = candidate(other, loss)
		result.Reason = "Niejednoznaczny obraz: co najmniej dwa miejsca pasują podobnie. Użyj większego wycinka lub przesuń postać."
		return result, nil
	}
	result.Found = true
	p := result.Best.Position
	result.Position = &p
	result.Reason = "Znaleziono pozycję na wybranym piętrze."
	return result, nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
