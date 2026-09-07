package brain

import (
	"encoding/binary"
	"image"
	"image/color"
	"strings"
	"testing"

	"minimap-lab/internal/frame"
	"minimap-lab/internal/vision"
)

// Bars are painted locally here, the same way as in internal/vision's and
// internal/battle's own tests. A shared helper in testenv would be DRY, but
// each of these tests checks a different geometry, and a local painter reads
// better than a function with five parameters. This duplication is
// deliberate, not an oversight.
func paintBar(im *image.NRGBA, g vision.Geometry, at image.Point, fill int, c vision.Color) {
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			im.SetNRGBA(at.X+x, at.Y+y, color.NRGBA{A: 255})
		}
	}
	for y := 0; y < g.InnerHeight(); y++ {
		for x := 0; x < fill; x++ {
			im.SetNRGBA(at.X+g.Border+x, at.Y+g.Border+y,
				color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255})
		}
	}
}

func filled(w, h int, c color.NRGBA) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, c)
		}
	}
	return im
}

// visionCalibration matches internal/brain/combatconfig_test.go's calibrated():
// a 240x176 game window at 16px per tile, cropped to the middle 11x11 tiles.
func visionCalibration() CombatConfig {
	c := calibrated()
	c.HasSelfBar, c.SelfBarX, c.SelfBarY = true, 81, 74
	return c
}

// crop paints the character's own bar plus one creature per offset given in
// whole tiles, and returns the crop as raw RGBA the way the panel sends it.
func crop(offsets ...image.Point) *image.NRGBA {
	c := visionCalibration()
	g := vision.Geometry{Width: c.BarWidth, Height: c.BarHeight, Border: c.BarBorder}
	im := filled(c.Crop.W, c.Crop.H, color.NRGBA{R: 100, G: 100, B: 100, A: 255})
	green := vision.DefaultColors()[0]
	paintBar(im, g, image.Pt(c.SelfBarX, c.SelfBarY), g.InnerWidth(), green)
	for _, o := range offsets {
		paintBar(im, g, image.Pt(c.SelfBarX+16*o.X, c.SelfBarY+16*o.Y), 5, green)
	}
	return im
}

// region packs one image the way frame.Parse expects it.
type region struct {
	id frame.RegionID
	im *image.NRGBA
}

func (h *harness) visionFrame(t *testing.T, regions ...region) frame.Frame {
	t.Helper()
	h.seq++
	h.videoUS += 100_000
	specs := append([]region{{id: frame.RegionMinimap, im: filled(2, 2, color.NRGBA{})}}, regions...)
	body := make([]byte, frame.HeaderSize+frame.RegionHeader*len(specs))
	copy(body[0:4], frame.Magic)
	body[4], body[5] = frame.FormatVersion, byte(len(specs))
	binary.LittleEndian.PutUint64(body[16:], h.seq)
	binary.LittleEndian.PutUint64(body[24:], h.videoUS)
	for i, r := range specs {
		hdr := body[frame.HeaderSize+frame.RegionHeader*i:]
		hdr[0] = byte(r.id)
		binary.LittleEndian.PutUint16(hdr[4:], uint16(r.im.Bounds().Dx()))
		binary.LittleEndian.PutUint16(hdr[6:], uint16(r.im.Bounds().Dy()))
		binary.LittleEndian.PutUint32(hdr[8:], uint32(len(r.im.Pix)))
	}
	for _, r := range specs {
		body = append(body, r.im.Pix...)
	}
	f, err := frame.Parse(body)
	if err != nil {
		t.Fatalf("frame.Parse: %v", err)
	}
	return f
}

// submit posts one frame and waits until its match has landed (see await).
func (h *harness) submit(t *testing.T, f frame.Frame) *State {
	t.Helper()
	h.loop.Submit(f, h.clock.now())
	return h.await(t, f.Seq)
}

func TestCombatCountsCreaturesInRadius(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.at(1000, 1000)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport,
		crop(image.Pt(1, 0), image.Pt(3, 0))}))
	if !s.Combat.Calibrated {
		t.Fatal("stan walki musi być oznaczony jako skalibrowany")
	}
	if s.Combat.BarsTotal != 2 {
		t.Errorf("BarsTotal = %d, oczekiwano 2 (własny pasek jest wykluczany)", s.Combat.BarsTotal)
	}
	if s.Combat.MonstersInRange != 2 {
		t.Errorf("MonstersInRange = %d, oczekiwano 2 przy promieniu 4", s.Combat.MonstersInRange)
	}
}

func TestCombatRadiusExcludesFartherCreature(t *testing.T) {
	h := newHarness(t)
	c := visionCalibration()
	c.DecisionRadius = 2
	// A smaller radius needs a smaller recommended crop, and our crop is
	// larger, so validation still passes.
	h.config(t, func(cfg *Config) { cfg.Combat = c })
	h.at(1000, 1000)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport,
		crop(image.Pt(1, 0), image.Pt(3, 0))}))
	if s.Combat.BarsTotal != 2 {
		t.Errorf("BarsTotal = %d, oczekiwano 2", s.Combat.BarsTotal)
	}
	if s.Combat.MonstersInRange != 1 {
		t.Errorf("MonstersInRange = %d, oczekiwano 1 przy promieniu 2", s.Combat.MonstersInRange)
	}
}

func TestCombatSurvivesLostPosition(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.locator.miss()
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))}))
	if s.Position != nil {
		t.Fatal("test wymaga utraconego dopasowania minimapy")
	}
	if s.Combat.MonstersInRange != 1 {
		t.Errorf("MonstersInRange = %d, oczekiwano 1 — liczenie potworów nie potrzebuje pozycji",
			s.Combat.MonstersInRange)
	}
}

// The map sieve is the whole reason observeVision and finishVision are split
// and the frame handler defers finishVision to the end: a bar drawn from
// another floor must not inflate the count. Without this test the sieve ran
// on every other test in this file with the tile forced walkable, so
// blockedTile's math.Round conversion, its choice of p.Z, and its position-nil
// short-circuit were all unverified - and the failure mode of a bug there is
// silent under-reporting of monsters, the direction combatconfig.go itself
// calls the most dangerous.
func TestCombatSieveRejectsBarsOnBlockedTiles(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.setTile(TileBlocked)
	h.at(1000, 1000)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport,
		crop(image.Pt(1, 0), image.Pt(3, 0))}))
	if s.Combat.RejectedByMap != 2 {
		t.Errorf("RejectedByMap = %d, oczekiwano 2", s.Combat.RejectedByMap)
	}
	if s.Combat.BarsTotal != 0 {
		t.Errorf("BarsTotal = %d, oczekiwano 0 — obie kratki są nieprzechodnie", s.Combat.BarsTotal)
	}
	if s.Combat.MonstersInRange != 0 {
		t.Errorf("MonstersInRange = %d, oczekiwano 0", s.Combat.MonstersInRange)
	}
}

// The documented contract is "always zero while the position is unknown,
// because the sieve has no tile to ask about" - pinned here rather than left
// as a comment nobody runs.
func TestCombatSieveNeedsAPositionToReject(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.setTile(TileBlocked)
	h.locator.miss()
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))}))
	if s.Combat.RejectedByMap != 0 {
		t.Errorf("RejectedByMap = %d, oczekiwano 0 — pozycja nieznana, sito nie ma czego pytać",
			s.Combat.RejectedByMap)
	}
	if s.Combat.BarsTotal != 1 {
		t.Errorf("BarsTotal = %d, oczekiwano 1", s.Combat.BarsTotal)
	}
}

func TestCombatIsSilentWithoutViewportRegion(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.at(1000, 1000)
	s := h.submit(t, h.visionFrame(t))
	if s.Combat.BarsTotal != 0 || s.Combat.MonstersInRange != 0 {
		t.Errorf("klatka bez regionu okna gry dała %+v", s.Combat)
	}
	if s.Position == nil {
		t.Error("brak regionu okna gry nie może przerwać lokalizacji")
	}
}

func TestCombatFlagsMixedCrowd(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.at(1000, 1000)
	c := visionCalibration()
	g := vision.Geometry{Width: c.BattleBarWidth, Height: c.BattleBarHeight, Border: c.BattleBarBorder}
	list := filled(c.Battle.W, c.Battle.H, color.NRGBA{R: 60, G: 60, B: 60, A: 255})
	paintBar(list, g, image.Pt(4, 4), 8, vision.DefaultColors()[0])
	s := h.submit(t, h.visionFrame(t,
		region{frame.RegionViewport, crop(image.Pt(1, 0), image.Pt(2, 0))},
		region{frame.RegionBattle, list}))
	if s.Combat.BattleRows != 1 {
		t.Fatalf("BattleRows = %d, oczekiwano 1", s.Combat.BattleRows)
	}
	if !s.Combat.MixedCrowd {
		t.Error("dwa paski przy jednym wierszu listy dowodzą, że w wycinku jest nie-potwór")
	}
}

func TestVitalsReachTheSnapshot(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.at(1000, 1000)
	hp := filled(100, 8, color.NRGBA{R: 20, G: 20, B: 20, A: 255})
	for y := 0; y < 8; y++ {
		for x := 0; x < 40; x++ {
			hp.SetNRGBA(x, y, color.NRGBA{G: 200, A: 255})
		}
	}
	s := h.submit(t, h.visionFrame(t, region{frame.RegionHP, hp}))
	if !s.Combat.HPOK {
		t.Fatalf("odczyt HP odrzucony: %s", s.Combat.Reason)
	}
	if s.Combat.HPPct < 0.38 || s.Combat.HPPct > 0.42 {
		t.Errorf("HP = %.3f, oczekiwano około 0,4", s.Combat.HPPct)
	}
}

func TestVisionSnapshotCarriesOffsetsButStateDoesNot(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.at(1000, 1000)
	// Two creatures, one inside the default radius (4) and one past it, so
	// BarView.InRange can be checked both ways from a single frame.
	h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0), image.Pt(5, 0))}))
	view := h.loop.VisionSnapshot(h.ctx)
	if !view.Have || len(view.Bars) != 2 {
		t.Fatalf("podgląd widzenia: %+v", view)
	}
	if dx := view.Bars[0].DX; dx < 0.9 || dx > 1.1 {
		t.Errorf("offset dx = %.3f, oczekiwano około 1", dx)
	}
	// InRange must be the same answer finishVision uses for MonstersInRange
	// (dist <= DecisionRadius+0.5 = 4.5 here), not re-derived by the panel.
	if !view.Bars[0].InRange {
		t.Errorf("bliski stwór (dist ≈ 1) powinien mieć InRange = true")
	}
	if view.Bars[1].InRange {
		t.Errorf("daleki stwór (dist ≈ 5) powinien mieć InRange = false")
	}
	data, err := marshalState(h.loop.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	// The snapshot is meant to stay small: bar rectangles travel only through
	// /api/vision.
	for _, forbidden := range []string{`"dx"`, `"fill"`} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("snapshot wiezie %s, a nie powinien: %s", forbidden, data)
		}
	}
}

// Disabling calibration through SetConfig must clear Combat right away,
// rather than leaving the stale counts published until the next frame -
// which may never arrive if the camera has stopped sending. This is why the
// test reads the snapshot straight after SetConfig, with no frame submitted
// in between: going through submit/await first would hide exactly the bug
// this test exists to catch.
func TestCombatClearsOnConfigDisable(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.at(1000, 1000)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))}))
	if !s.Combat.Calibrated || s.Combat.MonstersInRange == 0 {
		t.Fatalf("test wymaga skalibrowanego stanu z niezerową liczbą potworów, dostał: %+v", s.Combat)
	}

	h.config(t, func(c *Config) {})

	got := h.loop.Snapshot()
	if got.Combat.Calibrated {
		t.Error("Calibrated wciąż true po wyłączeniu kalibracji, mimo że nie doszła żadna nowa klatka")
	}
	if got.Combat.BarsTotal != 0 || got.Combat.MonstersInRange != 0 {
		t.Errorf("stan walki po wyłączeniu kalibracji = %+v, oczekiwano samych zer", got.Combat)
	}
}
