package fight_test

import (
	"testing"
	"time"

	"minimap-lab/internal/fight"
)

func TestTargeterTapsImmediatelyWhenNoTargetEverSeen(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: base}, opts())
	if !d.Tap {
		t.Fatalf("pierwsze stuknięcie musi polecieć od razu, dostałem %+v", d)
	}
}

func TestTargeterWaitsRetryAfterEmission(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: base}, opts())
	tg.Tapped(base)
	d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: base.Add(300 * time.Millisecond)}, opts())
	if d.Tap {
		t.Fatal("drugie stuknięcie przed upływem target_retry_ms nie może polecieć")
	}
	d = tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: base.Add(600 * time.Millisecond)}, opts())
	if !d.Tap {
		t.Fatal("po dokładnie target_retry_ms od emisji drugie stuknięcie powinno polecieć")
	}
}

func TestTargeterOneFrameFlickerDoesNotRetap(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	// The frame is visible, then vanishes for exactly one frame.
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 1, Rows: 1, CapturedAt: base}, opts())
	d := tg.Decide(fight.TargetInput{HasTarget: false, Rows: 1, CapturedAt: base.Add(100 * time.Millisecond)}, opts())
	if d.Tap {
		t.Fatal("jednoklatkowy zanik ramki nie może stuknąć - anchor liczy się od ostatniego widzenia")
	}
}

func TestTargeterRefusalDoesNotCountAsAttempt(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	// Five decisions to tap, but Tapped is never called (as if the driver
	// refused every one) - attempts must stay at zero, so no backoff fires.
	for i := 0; i < 5; i++ {
		at := base.Add(time.Duration(i) * 700 * time.Millisecond)
		d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
		if !d.Tap {
			t.Fatalf("próba %d: bez potwierdzonych emisji nigdy nie powinniśmy wpaść w backoff, dostałem %+v", i, d)
		}
	}
}

func TestTargeterBacksOffAfterThreeEmissionsWithoutFrame(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	at := base
	for i := 0; i < 3; i++ {
		d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
		if !d.Tap {
			t.Fatalf("emisja %d powinna polecieć, dostałem %+v", i, d)
		}
		tg.Tapped(at)
		at = at.Add(700 * time.Millisecond)
	}
	d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
	if d.Tap {
		t.Fatal("po trzech emisjach bez ramki czwarta próba musi wejść w backoff, nie stuknąć")
	}
	// Still inside target_backoff_ms (2s default).
	d = tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at.Add(500 * time.Millisecond)}, opts())
	if d.Tap {
		t.Fatal("w trakcie backoffu nie wolno stukać")
	}
	// Past the backoff window: a fresh set of attempts.
	d = tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at.Add(2100 * time.Millisecond)}, opts())
	if !d.Tap {
		t.Fatal("po upływie backoffu celownik powinien znów spróbować")
	}
}

func TestTargeterFrameResetsAttempts(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	at := base
	for i := 0; i < 2; i++ {
		tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
		tg.Tapped(at)
		at = at.Add(700 * time.Millisecond)
	}
	// The frame appears - attempts must reset to zero.
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 1, Rows: 1, CapturedAt: at}, opts())
	// The sighting is also the retry anchor, so the next probe has to wait out
	// target_retry_ms just like any other - see the one-frame flicker test.
	at = at.Add(700 * time.Millisecond)
	// It vanishes again; three MORE fruitless emissions must be needed before
	// backing off again, not just one: without the reset the second pass below
	// would already be the fourth attempt and would back off instead of tapping.
	for i := 0; i < 2; i++ {
		d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
		if !d.Tap {
			t.Fatalf("próba %d po zresetowaniu powinna polecieć, dostałem %+v", i, d)
		}
		tg.Tapped(at)
		at = at.Add(700 * time.Millisecond)
	}
}

func TestTargeterStallsAfterFifteenSecondsWithoutHPDrop(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.9, Rows: 1, CapturedAt: base}, opts())
	d := tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.9, Rows: 1,
		CapturedAt: base.Add(14 * time.Second)}, opts())
	if d.Stall {
		t.Fatal("14 s bez spadku HP to jeszcze nie zastój (próg to 15 s)")
	}
	d = tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.9, Rows: 1,
		CapturedAt: base.Add(15 * time.Second)}, opts())
	if !d.Stall {
		t.Fatal("15 s bez spadku minimalnego HP musi zgłosić zastój")
	}
}

func TestTargeterHPDropResetsStall(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.9, Rows: 1, CapturedAt: base}, opts())
	// HP drops at 10s - a genuine hit, must reset stallSince.
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.7, Rows: 1,
		CapturedAt: base.Add(10 * time.Second)}, opts())
	d := tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.7, Rows: 1,
		CapturedAt: base.Add(24 * time.Second)}, opts())
	if d.Stall {
		t.Fatal("spadek HP w 10 s powinien zresetować licznik zastoju - 14 s później to za mało")
	}
}

func TestTargeterNewTargetResetsStall(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.5, Rows: 1, CapturedAt: base}, opts())
	// The target vanishes (killed or lost) and a new one appears later.
	tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: base.Add(1 * time.Second)}, opts())
	tg.Tapped(base.Add(1 * time.Second))
	newTarget := base.Add(14 * time.Second)
	d := tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 1, Rows: 1, CapturedAt: newTarget}, opts())
	if d.Stall {
		t.Fatal("nowy cel nie może odziedziczyć zastoju poprzedniego")
	}
	d = tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 1, Rows: 1,
		CapturedAt: newTarget.Add(14 * time.Second)}, opts())
	if d.Stall {
		t.Fatal("14 s od nowego celu to wciąż za mało na zastój")
	}
}

func TestTargeterAttemptsAndBackoffUntilGetters(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	if n := tg.Attempts(); n != 0 {
		t.Fatalf("świeży Targeter powinien mieć zero prób, dostałem %d", n)
	}
	if _, ok := tg.BackoffUntil(); ok {
		t.Fatal("świeży Targeter nie powinien mieć aktywnego backoffu")
	}
	at := base
	for i := 0; i < 3; i++ {
		tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
		tg.Tapped(at)
		at = at.Add(700 * time.Millisecond)
	}
	if n := tg.Attempts(); n != 3 {
		t.Fatalf("po trzech potwierdzonych emisjach oczekiwałem 3 prób, dostałem %d", n)
	}
	tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
	until, ok := tg.BackoffUntil()
	if !ok {
		t.Fatal("po czwartej próbie bez ramki backoff powinien być aktywny")
	}
	if want := at.Add(2 * time.Second); !until.Equal(want) {
		t.Fatalf("backoff powinien kończyć się o %v, dostałem %v", want, until)
	}
}
