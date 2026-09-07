package testenv

import (
	"image"
	"os"
	"testing"
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

// CombatCalibration reads testdata/combat-capture.png correctly.
func CombatCalibration() CombatFixture {
	return CombatFixture{
		Viewport:  image.Rect(0, 0, 0, 0), // ZMIERZ
		Crop:      image.Rect(0, 0, 0, 0), // ZMIERZ
		Battle:    image.Rect(0, 0, 0, 0), // ZMIERZ
		HP:        image.Rect(0, 0, 0, 0), // ZMIERZ
		Mana:      image.Rect(0, 0, 0, 0), // ZMIERZ
		GridCols:  15,
		GridRows:  11,
		SelfBar:   image.Point{}, // ZMIERZONE W ZADANIU 3
		Monsters:  0,             // POLICZ
		TargetRow: -1,            // ODCZYTAJ
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
