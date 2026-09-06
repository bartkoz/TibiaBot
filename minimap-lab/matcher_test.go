package main

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func demoOptions() Options {
	return Options{Zoom: 2, MarkerX: 94, MarkerY: 94, MaskRadius: 5, MinScore: .94, MinGap: .015}
}

func assertPosition(t *testing.T, result Result, want Position) {
	t.Helper()
	if !result.Found || result.Position == nil || *result.Position != want {
		t.Fatalf("want %+v, got %+v (best %+v, competitor %+v)", want, result, result.Best, result.Competitor)
	}
}

func TestDemoZoomAndMarkerMask(t *testing.T) {
	a := demoAtlas()
	result, err := locate(context.Background(), a, demoSnippet(a), demoOptions())
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, result, Position{32200, 32180, 7})
	if result.Best.Score != 1 {
		t.Fatalf("masked cross must not affect exact match: %v", result.Best.Score)
	}
}

func TestShiftedNonCenteredMarker(t *testing.T) {
	a := demoAtlas()
	// Marker is deliberately off center; moving source terrain changes the answer.
	im := image.NewNRGBA(image.Rect(0, 0, 95, 85))
	draw.Draw(im, im.Bounds(), a.Image, image.Pt(167, 111), draw.Src)
	o := Options{Zoom: 1, MarkerX: 22, MarkerY: 31, MaskRadius: 0, MinScore: .94, MinGap: .015}
	result, err := locate(context.Background(), a, im, o)
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, result, Position{32189, 32142, 7})
}

func TestDuplicateTerrainRejectsPosition(t *testing.T) {
	a := demoAtlas()
	draw.Draw(a.Image, image.Rect(10, 10, 105, 105), a.Image, image.Pt(153, 133), draw.Src)
	result, err := locate(context.Background(), a, demoSnippet(a), demoOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Found || result.Position != nil || result.Competitor == nil || result.Best.Score != 1 || result.Competitor.Score != 1 {
		t.Fatalf("duplicate must be ambiguous: %+v", result)
	}
}

func TestWrongImageRejectsPosition(t *testing.T) {
	a := demoAtlas()
	im := demoSnippet(a).(*image.NRGBA)
	for y := 0; y < im.Bounds().Dy(); y++ {
		for x := 0; x < im.Bounds().Dx(); x++ {
			c := im.NRGBAAt(x, y)
			im.SetNRGBA(x, y, color.NRGBA{255 - c.R, 255 - c.G, 255 - c.B, 255})
		}
	}
	result, err := locate(context.Background(), a, im, demoOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Found || result.Position != nil || result.Best == nil || result.Best.Score >= demoOptions().MinScore {
		t.Fatalf("unrelated image matched: %+v", result)
	}
}

func TestSingleColorCaveUsesBlackWalls(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 180, 160))
	draw.Draw(im, im.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	floor := image.NewUniform(color.NRGBA{153, 153, 153, 255})
	for _, r := range []image.Rectangle{image.Rect(25, 20, 45, 130), image.Rect(25, 90, 140, 110), image.Rect(95, 35, 115, 110), image.Rect(60, 45, 155, 65)} {
		draw.Draw(im, r, floor, image.Point{}, draw.Src)
	}
	a := &Atlas{im, image.Pt(32000, 32000), 8}
	crop := image.NewNRGBA(image.Rect(0, 0, 95, 95))
	draw.Draw(crop, crop.Bounds(), im, image.Pt(45, 20), draw.Src)
	o := Options{Zoom: 1, MarkerX: 47, MarkerY: 47, MaskRadius: 5, MinScore: .94, MinGap: .015}
	result, err := locate(context.Background(), a, crop, o)
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, result, Position{32092, 32067, 8})
}

func TestUniformAndInvalidInputs(t *testing.T) {
	uniform := image.NewNRGBA(image.Rect(0, 0, 95, 95))
	draw.Draw(uniform, uniform.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	valid := Options{Zoom: 1, MarkerX: 47, MarkerY: 47, MaskRadius: 5, MinScore: .94, MinGap: .015}
	if _, err := makeSamples(uniform, valid); err == nil {
		t.Fatal("uniform image accepted")
	}
	for _, change := range []func(*Options){func(o *Options) { o.Zoom = 0 }, func(o *Options) { o.MarkerX = 95 }, func(o *Options) { o.MaskRadius = -1 }, func(o *Options) { o.MinScore = 2 }, func(o *Options) { o.MinGap = 0 }} {
		o := valid
		change(&o)
		if _, err := makeSamples(uniform, o); err == nil {
			t.Fatalf("invalid options accepted: %+v", o)
		}
	}
}

func TestCanceledSearch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := locate(ctx, demoAtlas(), demoSnippet(demoAtlas()), demoOptions())
	if err != context.Canceled {
		t.Fatalf("got %v", err)
	}
}

func savePNG(t testing.TB, path string, im image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, im)
	closeErr := f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestAtlasSeamAndMissingChunk(t *testing.T) {
	dir := t.TempDir()
	terrain := demoAtlas().Image
	left := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	draw.Draw(left, left.Bounds(), terrain, image.Point{}, draw.Src)
	right := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	draw.Draw(right, right.Bounds(), terrain, image.Pt(256, 0), draw.Src)
	savePNG(t, filepath.Join(dir, "Minimap_Color_32000_32000_7.png"), left)
	savePNG(t, filepath.Join(dir, "Minimap_Color_32256_32000_7.png"), right)
	a, err := loadAtlas(dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	im := image.NewNRGBA(image.Rect(0, 0, 95, 95))
	draw.Draw(im, im.Bounds(), terrain, image.Pt(210, 110), draw.Src)
	o := Options{Zoom: 1, MarkerX: 47, MarkerY: 47, MaskRadius: 5, MinScore: .94, MinGap: .015}
	result, err := locate(context.Background(), a, im, o)
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, result, Position{32257, 32157, 7})
	if _, err := loadAtlas(dir, 8); err == nil {
		t.Fatal("missing floor accepted")
	}
	// Missing half of a matching region must not turn into matching black terrain.
	draw.Draw(a.Image, image.Rect(256, 0, 512, 256), image.Transparent, image.Point{}, draw.Src)
	result, err = locate(context.Background(), a, im, o)
	if err != nil {
		t.Fatal(err)
	}
	if result.Found {
		t.Fatal("matched across missing chunk")
	}
}

func TestLocalMapIntegration(t *testing.T) {
	if os.Getenv("MINIMAP_REAL_MAP_TEST") != "1" {
		t.Skip("set MINIMAP_REAL_MAP_TEST=1 to test existing local maps")
	}
	a, err := loadAtlas("../data/minimap", 7)
	if err != nil {
		t.Fatal(err)
	}
	want := Position{32369, 32241, 7}
	im := image.NewNRGBA(image.Rect(0, 0, 95, 95))
	draw.Draw(im, im.Bounds(), a.Image, image.Pt(want.X-47, want.Y-47).Sub(a.Origin), draw.Src)
	result, err := locate(context.Background(), a, im, Options{Zoom: 1, MarkerX: 47, MarkerY: 47, MaskRadius: 5, MinScore: .94, MinGap: .015})
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, result, want)
}
