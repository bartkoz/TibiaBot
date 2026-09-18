package testenv

import (
	"image"
	"image/draw"
	"os"
	"testing"

	"minimap-lab/internal/vision"
)

// CombatFixture carries the panel settings testdata/combat-capture.png was
// captured with, and what a human counted on it. It lives here for the same
// reason VenoreCalibration does: three packages build the same fixture from
// these numbers, and that only works as a cross-check while all of them use
// the same ones.
type CombatFixture struct {
	Viewport image.Rectangle
	Crop     image.Rectangle
	Battle   image.Rectangle
	HP       image.Rectangle
	Mana     image.Rectangle

	GridCols, GridRows int

	// SelfBar is the top-left corner of the character's own health bar inside
	// Crop. The camera is centred on the character, so it never moves.
	SelfBar image.Point

	// Monsters is the number of creature bars visible in Crop excluding the
	// character's own bar, and TargetRow which battle list entry carried the
	// attack frame, counting from zero; -1 when none did.
	Monsters  int
	TargetRow int

	// BarGeometry and the tolerances below are the game-window calibration
	// this capture actually needs. They are here rather than hardcoded in the
	// tests because the numbers are a property of the capture, and because
	// three packages have to agree on them for the cross-check to mean
	// anything.
	BarGeometry  vision.Geometry
	BarTolerance int
	BlackMax     int
	BarEdge      int

	// BarColors are the measured fill colours, shared by both detectors the
	// way CombatConfig.BarColors is. They are not vision.DefaultColors():
	// those are brighter than anything this client draws, and calibrating to
	// them would need a tolerance wide enough to swallow the scenery.
	BarColors []vision.Color

	// The battle list is drawn by the panel, not by the lit game world, so
	// the same colours come out brighter there and the unfilled part of a bar
	// is grey rather than black. That is why it has its own tolerance and
	// black level rather than borrowing the game window's.
	BattleGeometry  vision.Geometry
	BattleTolerance int
	BattleBlackMax  int
	BattleEdge      int
	RowPitch        int

	// Frame is the attack border the client draws round the targeted entry's
	// icon, and IconOffsetX/Y plus IconSize say where that icon sits relative
	// to the entry's health bar.
	Frame          vision.Color
	FrameTolerance int
	FrameCoverage  float64
	IconOffsetX    int
	IconOffsetY    int
	IconSize       int
}

// Rects names every measured rectangle, for tests that check all of them.
func (f CombatFixture) Rects() map[string]image.Rectangle {
	return map[string]image.Rectangle{
		"okno gry":     f.Viewport,
		"wycinek":      f.Crop,
		"battle lista": f.Battle,
		"pasek HP":     f.HP,
		"pasek many":   f.Mana,
	}
}

// Measured reports whether a human has filled in the measurements yet. Until
// they have, every test that needs the real capture skips itself.
func (f CombatFixture) Measured() bool { return !f.Viewport.Empty() }

// CombatCalibration reads testdata/combat-capture.png correctly. Every number
// here was measured on that 5120x2880 native-resolution capture and confirmed
// by running the real detectors over it - see
// docs/superpowers/plans/2026-09-07-vision-layer-measurements.md.
func CombatCalibration() CombatFixture {
	return CombatFixture{
		Viewport: image.Rect(637, 245, 3778, 2548),
		Crop:     image.Rect(1056, 245, 3359, 2548),
		Battle:   image.Rect(4770, 900, 5100, 1150),
		HP:       image.Rect(24, 134, 2202, 136),
		Mana:     image.Rect(2217, 134, 4392, 136),
		GridCols: 15,
		GridRows: 11,
		// SelfBar is the blue bar the client draws under the character's own
		// health bar, measured from pixels rather than from a detected bar -
		// blue is not a health colour, so no detector reports it. Exclude is
		// therefore a no-op on this capture twice over: the character's green
		// bar eight pixels above reads #52a452, the shade for roughly 90%
		// health, which BarColors does not list either. Keep that in mind if
		// BarColors ever grows that shade - the monster count would become
		// four, and this point would have to move to the green bar's corner
		// at (1070, 977) to bring it back to three. The anchor derived from
		// it is off by those eight pixels, four hundredths of a tile, which
		// the offset test's bounds check tolerates.
		SelfBar:   image.Point{X: 1070, Y: 985},
		Monsters:  3,
		TargetRow: 0,

		BarGeometry:  vision.Geometry{Width: 62, Height: 8, Border: 3},
		BarTolerance: 20,
		BlackMax:     125,
		BarEdge:      0,

		// Measured cores: green #00a100/#009500, yellow #a1a100 - the world's
		// lighting darkens them, and the client's antialiasing flattens the
		// peak further, so the calibration point sits a little below the
		// brightest pixel of each.
		BarColors: []vision.Color{
			{R: 0x00, G: 0x9b, B: 0x00},
			{R: 0x9b, G: 0x9b, B: 0x00},
			{R: 0xaa, G: 0x0a, B: 0x0a},
		},

		BattleGeometry:  vision.Geometry{Width: 262, Height: 8, Border: 1},
		BattleTolerance: 60,
		BattleBlackMax:  85,
		BattleEdge:      1,
		RowPitch:        44,
		Frame:           vision.Color{R: 0xc9, G: 0x0a, B: 0x0a},
		FrameTolerance:  40,
		FrameCoverage:   0.8,
		IconOffsetX:     -45,
		IconOffsetY:     -31,
		IconSize:        40,
	}
}

// CombatCapture decodes the real game capture, skipping the test when it has
// not been recorded yet. It is the same gate MapDir uses for the downloaded
// minimap pack: data that cannot live in the repository, or has not been
// produced yet, must skip rather than fail - a red suite nobody can fix
// teaches the reader to ignore red.
func CombatCapture(t testing.TB) image.Image {
	t.Helper()
	if !CombatCalibration().Measured() {
		t.Skip("brak pomiarów w testenv.CombatCalibration — zgraj klatkę z gry przyciskiem " +
			"„Zapisz klatkę PNG”, zapisz ją jako testdata/combat-capture.png i wpisz zmierzone prostokąty")
	}
	if _, err := os.Stat(FixturePath(t, "combat-capture.png")); err != nil {
		t.Skip("brak testdata/combat-capture.png — zgraj klatkę z gry przyciskiem „Zapisz klatkę PNG”")
	}
	return LoadFixture(t, "combat-capture.png")
}

// NRGBACrop cuts a rectangle out of a fixture and converts it to NRGBA - the
// layout the frame protocol delivers, and the one every detector expects.
// Going through draw.Draw rather than asserting the decoded type matters: a
// PNG can decode as paletted, and reading a paletted image's bytes as if they
// were colours is how this project once made every wall look walkable.
func NRGBACrop(t testing.TB, im image.Image, r image.Rectangle) *image.NRGBA {
	t.Helper()
	if r.Empty() {
		t.Fatalf("prostokąt wycinka jest pusty: %v — zmierz go w CombatCalibration", r)
	}
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), im, r.Min, draw.Src)
	return out
}
