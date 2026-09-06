package brain

import (
	"testing"
	"time"

	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
)

// base and at() reproduce the millisecond timeline the JavaScript tests used,
// where performance.now() counts from document start.
var base = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return base.Add(time.Duration(ms) * time.Millisecond) }

func foundAt(mode string) locate.Result {
	return locate.Result{Found: true, Position: &mapdata.Position{X: 32958, Y: 32077, Z: 7},
		Zoom: 1, Mode: mode, MatchMS: 3}
}

// The radius has to grow with how long ago the anchor was captured: the
// character kept walking while the match ran, so the window must cover
// wherever it could have reached. Calibration that no longer matches the
// anchor invalidates it outright - a different zoom is a different picture.
func TestTrackerRadiusGrowsWithAgeAndRejectsMismatchedCalibration(t *testing.T) {
	tr := NewTracker()
	tr.Observe(foundAt("local"), at(100), at(105), 5*time.Millisecond)

	for _, c := range []struct{ now, want int }{
		{200, 5},
		{1100, 22},
		{5000, 64},
	} {
		h, ok := tr.Hint(at(c.now), 7, 1, 20)
		if !ok || h.Radius != c.want {
			t.Errorf("Hint(%d) = %d, ok=%v; oczekiwano %d", c.now, h.Radius, ok, c.want)
		}
	}
	if h, ok := tr.Hint(at(200), 8, 1, 20); !ok || h.Near.Z != 7 {
		t.Errorf("sąsiednie piętro powinno zachować kotwicę na Z=7, dostano %+v ok=%v", h, ok)
	}
	for _, c := range []struct {
		name             string
		now, floor, zoom int
	}{
		{"piętro dalej niż o jedno", 200, 9, 1},
		{"inna skala", 200, 7, 2},
		{"kotwica starsza niż 30 s bez zasiewu", 40000, 7, 1},
	} {
		if _, ok := tr.Hint(at(c.now), c.floor, c.zoom, 20); ok {
			t.Errorf("%s: oczekiwano braku podpowiedzi", c.name)
		}
	}
}

// A global acquisition may be seconds old by the time it lands, and its age
// must survive: it is what tells the next match how wide to look. The local
// confirmation that follows narrows the window back down.
func TestTrackerGlobalAcquisitionKeepsItsAgeThenNarrows(t *testing.T) {
	tr := NewTracker()
	tr.Observe(foundAt("global"), at(0), at(13000), 13*time.Second)

	if h, ok := tr.Hint(at(13000), 7, 1, 20); !ok || h.Radius != 64 {
		t.Errorf("promień po zasiewie = %d ok=%v, oczekiwano 64", h.Radius, ok)
	}
	if s := tr.Stats(at(13000)); !s.HasAge || s.AgeMS != 13000 {
		t.Errorf("wiek kotwicy = %d has=%v, oczekiwano 13000", s.AgeMS, s.HasAge)
	}
	tr.Observe(foundAt("local"), at(13100), at(13105), 5*time.Millisecond)
	if h, ok := tr.Hint(at(13200), 7, 1, 20); !ok || h.Radius != 5 {
		t.Errorf("promień po potwierdzeniu = %d ok=%v, oczekiwano 5", h.Radius, ok)
	}
}

// Three consecutive misses mean the anchor is no longer trustworthy; each one
// before that widens the window instead, because a single missed frame is not
// evidence that the character teleported.
func TestTrackerWidensOnMissesThenGivesUp(t *testing.T) {
	tr := NewTracker()
	tr.Observe(foundAt("local"), at(0), at(5), 5*time.Millisecond)
	miss := locate.Result{Found: false, Mode: "local"}
	tr.Observe(miss, at(100), at(105), 5*time.Millisecond)
	h, ok := tr.Hint(at(100), 7, 1, 20)
	if !ok || h.Radius != 9 {
		t.Errorf("po jednym chybieniu promień = %d ok=%v, oczekiwano 9", h.Radius, ok)
	}
	tr.Observe(miss, at(200), at(205), 5*time.Millisecond)
	tr.Observe(miss, at(300), at(305), 5*time.Millisecond)
	if _, ok := tr.Hint(at(300), 7, 1, 20); ok {
		t.Error("po trzech chybieniach kotwica powinna przestać obowiązywać")
	}
}

// A global miss is different in kind from a local one: it says the character
// is nowhere on the floor we believed in, so the anchor is simply wrong.
func TestTrackerGlobalMissDropsTheAnchorOutright(t *testing.T) {
	tr := NewTracker()
	tr.Observe(foundAt("local"), at(0), at(5), 5*time.Millisecond)
	tr.Observe(locate.Result{Found: false, Mode: "global"}, at(100), at(105), 5*time.Millisecond)
	if _, ok := tr.Hint(at(100), 7, 1, 20); ok {
		t.Error("chybienie globalne powinno skasować kotwicę")
	}
}

// Hz is only meaningful while readings are actually arriving; a stale last
// reading must report zero rather than a rate from a loop that has stopped.
func TestTrackerStatsReportNoRateWhenReadingsGoStale(t *testing.T) {
	tr := NewTracker()
	for i := 0; i < 4; i++ {
		ms := i * 100
		tr.Observe(foundAt("local"), at(ms), at(ms+5), 5*time.Millisecond)
	}
	if s := tr.Stats(at(320)); s.Hz < 9 || s.Hz > 11 {
		t.Errorf("Hz = %.2f, oczekiwano około 10", s.Hz)
	}
	if s := tr.Stats(at(1000)); s.Hz != 0 {
		t.Errorf("Hz przy nieświeżych odczytach = %.2f, oczekiwano 0", s.Hz)
	}
}
