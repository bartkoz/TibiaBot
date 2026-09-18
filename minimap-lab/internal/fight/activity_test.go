package fight_test

import (
	"testing"
	"time"

	"minimap-lab/internal/fight"
)

func opts() fight.Options {
	return fight.Options{
		Confirm: 150 * time.Millisecond, LeaveFight: 600 * time.Millisecond,
		TargetRetry: 600 * time.Millisecond, TargetBackoff: 2 * time.Second,
		TargetStall: 15 * time.Second, TargetAttempts: 3,
	}
}

// ready is a valid "fight now" observation; tests mutate copies of it.
func ready(at time.Time) fight.Observation {
	return fight.Observation{Enabled: true, InRange: 1, BattleRead: true, Rows: 1, CapturedAt: at}
}

func TestActivityEntersAfterConfirm(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	if tr := a.Observe(ready(base), opts()); tr != fight.None {
		t.Fatalf("pierwsza klatka nie może wejść od razu, dostałem %v", tr)
	}
	if a.State() != fight.Travelling {
		t.Fatal("stan po pierwszej klatce musi zostać Travelling")
	}
	tr := a.Observe(ready(base.Add(160*time.Millisecond)), opts())
	if tr != fight.Entered {
		t.Fatalf("druga klatka po 160 ms powinna wejść, dostałem %v", tr)
	}
	if a.State() != fight.Fighting {
		t.Fatal("stan po wejściu musi być Fighting")
	}
}

func TestActivityRequiresNonEmptyBattleList(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	o := ready(base)
	o.Rows = 0
	a.Observe(o, opts())
	o2 := ready(base.Add(160 * time.Millisecond))
	o2.Rows = 0
	if tr := a.Observe(o2, opts()); tr != fight.None {
		t.Fatalf("paski bez wierszy battle listy nie mogą wejść w walkę, dostałem %v", tr)
	}
	if a.State() != fight.Travelling {
		t.Fatal("bez wierszy stan musi zostać Travelling")
	}
}

func TestActivityUnknownBattleListBlocksEntryWithoutDrivingExit(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	// Enter fighting first with a real read.
	a.Observe(ready(base), opts())
	a.Observe(ready(base.Add(160*time.Millisecond)), opts())
	if a.State() != fight.Fighting {
		t.Fatal("setup: powinniśmy być w Fighting")
	}
	// A frame where the battle region simply did not arrive (BattleRead=false,
	// Rows=0) must not look like "no rows" and drive an exit.
	unknown := fight.Observation{Enabled: true, InRange: 1, BattleRead: false, Rows: 0,
		CapturedAt: base.Add(320 * time.Millisecond)}
	for i := 0; i < 10; i++ {
		unknown.CapturedAt = base.Add(320*time.Millisecond + time.Duration(i)*100*time.Millisecond)
		if tr := a.Observe(unknown, opts()); tr == fight.Left {
			t.Fatalf("nieodczytana battle lista nie może napędzać wyjścia (i=%d)", i)
		}
	}
	if a.State() != fight.Fighting {
		t.Fatal("nieodczytana battle lista nie może wypchnąć z Fighting")
	}
}

func TestActivityLeavesAfterConfirm(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	a.Observe(ready(base), opts())
	a.Observe(ready(base.Add(160*time.Millisecond)), opts())
	empty := fight.Observation{Enabled: true, BattleRead: true, Rows: 0, InRange: 0}
	empty.CapturedAt = base.Add(320 * time.Millisecond)
	if tr := a.Observe(empty, opts()); tr != fight.None {
		t.Fatalf("pierwsza pusta klatka nie może wyjść od razu, dostałem %v", tr)
	}
	// Frames must keep arriving inside gapReset, otherwise Presence rightly
	// restarts the streak and no duration was ever actually observed. The
	// real loop feeds frames far more often than this.
	empty.CapturedAt = base.Add(620 * time.Millisecond)
	if tr := a.Observe(empty, opts()); tr != fight.None {
		t.Fatalf("po 300 ms braku celu jest za wcześnie na wyjście, dostałem %v", tr)
	}
	empty.CapturedAt = base.Add(920 * time.Millisecond) // +600ms from since
	tr := a.Observe(empty, opts())
	if tr != fight.Left {
		t.Fatalf("po 600 ms braku celu powinniśmy wyjść, dostałem %v", tr)
	}
	if a.State() != fight.Travelling {
		t.Fatal("stan po wyjściu musi być Travelling")
	}
}

func TestActivityVisibleTargetFrameOverridesEmptyExit(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	a.Observe(ready(base), opts())
	a.Observe(ready(base.Add(160*time.Millisecond)), opts())
	// The creature fled out of decision radius, but the client is still
	// chasing it and the attack frame is still on the battle list row.
	fleeing := fight.Observation{Enabled: true, BattleRead: true, Rows: 1, InRange: 0, HasTarget: true}
	for i := 0; i < 10; i++ {
		fleeing.CapturedAt = base.Add(320*time.Millisecond + time.Duration(i)*200*time.Millisecond)
		if tr := a.Observe(fleeing, opts()); tr == fight.Left {
			t.Fatalf("widoczna ramka celu musi trzymać w walce (i=%d)", i)
		}
	}
	if a.State() != fight.Fighting {
		t.Fatal("z widoczną ramką celu nie wolno wyjść z Fighting")
	}
}

func TestActivityFlickerAtRadiusEdgeGivesAtMostOneEntryAndExitPerCycle(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	entries, exits := 0, 0
	for i := 0; i < 40; i++ {
		o := ready(base.Add(time.Duration(i) * 100 * time.Millisecond))
		if i%2 == 0 {
			o.InRange = 0
		}
		switch a.Observe(o, opts()) {
		case fight.Entered:
			entries++
		case fight.Left:
			exits++
		}
	}
	if entries > 1 || exits > 1 {
		t.Fatalf("migający stwór na granicy promienia dał %d wejść i %d wyjść w jednym cyklu, oczekiwano co najwyżej 1 i 1", entries, exits)
	}
}

func TestActivityBlockedPreventsEntry(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	o := ready(base)
	o.Blocked = true
	a.Observe(o, opts())
	o2 := ready(base.Add(160 * time.Millisecond))
	o2.Blocked = true
	if tr := a.Observe(o2, opts()); tr != fight.None {
		t.Fatalf("Blocked musi zablokować wejście, dostałem %v", tr)
	}
	if a.State() != fight.Travelling {
		t.Fatal("zablokowane wejście musi zostawić stan Travelling")
	}
}

func TestActivityForce(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	a.Observe(ready(base), opts())
	a.Observe(ready(base.Add(160*time.Millisecond)), opts())
	if a.State() != fight.Fighting {
		t.Fatal("setup: powinniśmy być w Fighting")
	}
	a.Force(fight.Travelling)
	if a.State() != fight.Travelling {
		t.Fatal("Force musi ustawić stan natychmiast")
	}
	// Force must also clear the entry debounce: a single frame right after
	// Force must not enter immediately, as if confirm had been running for a
	// long time already.
	if tr := a.Observe(ready(base.Add(200*time.Millisecond)), opts()); tr != fight.None {
		t.Fatalf("po Force wejście musi znowu przejść pełny debounce, dostałem %v", tr)
	}
}
