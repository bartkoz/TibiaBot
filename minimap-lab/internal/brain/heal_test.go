package brain

import (
	"image/color"
	"testing"
	"time"

	"minimap-lab/internal/frame"
	"minimap-lab/internal/heal"
	"minimap-lab/internal/route"
)

// healingConfig is the setup every test here starts from: the vision
// calibration (the client's bars belong to it) plus one health-potion rule.
func healingConfig(c *Config) {
	c.Combat = visionCalibration()
	c.Heal = HealConfig{Enabled: true, Rules: []heal.Rule{{
		Enabled: true, Resource: heal.ResourceHP, BelowPct: 60,
		Hotkey: "f1", CooldownMS: 1000,
	}}}
}

// barAt builds one bar region filled to the given fraction, exactly the way
// TestVitalsReachTheSnapshot builds one: a dark strip with a lit prefix, which
// is what vitals.Read measures.
func barAt(id frame.RegionID, pct float64) region {
	const w, h = 100, 8
	im := filled(w, h, color.NRGBA{R: 20, G: 20, B: 20, A: 255})
	for y := 0; y < h; y++ {
		for x := 0; x < int(pct*w); x++ {
			im.SetNRGBA(x, y, color.NRGBA{G: 200, A: 255})
		}
	}
	return region{id, im}
}

func bars(hpPct, manaPct float64) []region {
	return []region{barAt(frame.RegionHP, hpPct), barAt(frame.RegionMana, manaPct)}
}

// Healing runs before the match and regardless of it: the client's camera is
// centred on the character, so the bars sit in the same pixels whether or not
// anyone knows where she is standing.
func TestHealsWithoutAPosition(t *testing.T) {
	h := newHarness(t)
	h.config(t, healingConfig)
	h.locator.miss()

	f := h.visionFrame(t, bars(0.3, 1)...)
	h.loop.Submit(f, h.clock.now())
	s := h.awaitFrame(t, f.Seq)

	if got := h.ctrl.healKeys(); len(got) != 1 || got[0] != "f1" {
		t.Fatalf("klawisze leczenia = %v", got)
	}
	if s.Heal.LastHotkey != "f1" {
		t.Fatalf("stan leczenia = %+v", s.Heal)
	}
}

// A full search that found nothing stops the search, not the bot. Healing has
// to survive that gate: the character keeps taking damage either way.
func TestHealsAfterTheSearchGaveUp(t *testing.T) {
	h := newHarness(t)
	h.config(t, healingConfig)
	h.locator.miss()

	first := h.visionFrame(t, bars(0.3, 1)...)
	h.loop.Submit(first, h.clock.now())
	h.await(t, first.Seq) // the failed match has been applied, so the search is off

	h.clock.advance(1100 * time.Millisecond) // past the rule cooldown and the shared gap
	second := h.visionFrame(t, bars(0.3, 1)...)
	h.loop.Submit(second, h.clock.now())
	h.awaitFrame(t, second.Seq)

	if got := h.ctrl.healKeys(); len(got) != 2 {
		t.Fatalf("klawisze leczenia = %v, oczekiwano dwóch", got)
	}
}

func TestDoesNotHealAboveTheThreshold(t *testing.T) {
	h := newHarness(t)
	h.config(t, healingConfig)
	f := h.visionFrame(t, bars(0.9, 1)...)
	h.loop.Submit(f, h.clock.now())
	h.awaitFrame(t, f.Seq)
	if got := h.ctrl.healKeys(); len(got) != 0 {
		t.Fatalf("leczenie przy pełnym HP: %v", got)
	}
}

// The same picture twice is one observation. Healing on a repeat would drink
// twice off a single look at the screen.
func TestRepeatedFrameDoesNotHealTwice(t *testing.T) {
	h := newHarness(t)
	h.config(t, healingConfig)
	f := h.visionFrame(t, bars(0.3, 1)...)
	h.loop.Submit(f, h.clock.now())
	h.awaitFrame(t, f.Seq)

	h.videoUS -= 100_000 // the same picture, only a new sequence number
	again := h.visionFrame(t, bars(0.3, 1)...)
	h.loop.Submit(again, h.clock.now())
	h.awaitFrame(t, again.Seq)

	if got := h.ctrl.healKeys(); len(got) != 1 {
		t.Fatalf("klawisze leczenia = %v, oczekiwano jednego", got)
	}
}

// A refused tap changed nothing on screen, so it must not burn the cooldown -
// the next frame has to try again.
func TestRefusedHealDoesNotBurnTheCooldown(t *testing.T) {
	h := newHarness(t)
	h.config(t, healingConfig)
	h.ctrl.setStatus("refused")

	f := h.visionFrame(t, bars(0.3, 1)...)
	h.loop.Submit(f, h.clock.now())
	h.awaitFrame(t, f.Seq)

	h.ctrl.setStatus("emitted")
	h.clock.advance(100 * time.Millisecond)
	second := h.visionFrame(t, bars(0.3, 1)...)
	h.loop.Submit(second, h.clock.now())
	h.awaitFrame(t, second.Seq)

	if got := h.ctrl.healKeys(); len(got) != 2 {
		t.Fatalf("prób leczenia = %d, oczekiwano dwóch", len(got))
	}
}

// A healed frame does not also walk. The step is skipped before the executor
// is asked for an intent, so no pending step is left behind to time out.
func TestHealingPreemptsTheStep(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {
		healingConfig(c)
		c.Follow, c.Walk = true, true
	})
	h.loop.SetRoute(h.ctx, route.Route{Waypoints: []route.Waypoint{
		{X: 101, Y: 100, Z: 7, Type: "walk"}}})
	h.at(100, 100, 7)

	f := h.visionFrame(t, bars(0.3, 1)...)
	h.loop.Submit(f, h.clock.now())
	s := h.await(t, f.Seq)

	if keys, _ := h.ctrl.pressed(); len(keys) != 0 {
		t.Fatalf("bot poszedł w klatce, w której się leczył: %v", keys)
	}
	if got := h.ctrl.healKeys(); len(got) != 1 {
		t.Fatalf("klawisze leczenia = %v", got)
	}
	if s.Executor.Waiting {
		t.Error("został krok w toku, który wygaśnie w ponowienie i trwałą blokadę")
	}
}

func TestSwitchedOffHealingDoesNothing(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {
		healingConfig(c)
		c.Heal.Enabled = false
	})
	f := h.visionFrame(t, bars(0.3, 1)...)
	h.loop.Submit(f, h.clock.now())
	s := h.awaitFrame(t, f.Seq)
	if got := h.ctrl.healKeys(); len(got) != 0 {
		t.Fatalf("wyłączone leczenie jednak leczyło: %v", got)
	}
	if s.Heal.Enabled {
		t.Error("snapshot mówi, że leczenie jest włączone")
	}
}
