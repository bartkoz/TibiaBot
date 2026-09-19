package fight_test

import (
	"testing"
	"time"

	"minimap-lab/internal/fight"
	"minimap-lab/internal/vitals"
)

func fullMana() vitals.Reading { return vitals.Reading{Percent: 1, OK: true} }

// confirmedCrowd advances a fresh Engine's per-rule debounce until every
// rule's own condition (count >= MinMonsters within Radius) reads confirmed,
// then returns the capture time for the caller's actual assertion.
func confirmedCrowd(e *fight.Engine, crowd fight.Crowd, mana vitals.Reading, hasTarget, blockMixed bool, at time.Time, confirm time.Duration) time.Time {
	// Two observations, gapReset apart in spirit but well inside it, are
	// enough to confirm at any tested ConfirmMS in this file (150ms cases).
	e.Decide(crowd, mana, hasTarget, blockMixed, at, confirm)
	return at.Add(confirm + 10*time.Millisecond)
}

func TestEngineFirstFeasibleNotFirstMatching(t *testing.T) {
	e := fight.NewEngine()
	e.SetRules([]fight.Rule{
		{Enabled: true, Hotkey: "f2", MinMonsters: 4, Radius: 2, CooldownMS: 2000},
		{Enabled: true, Hotkey: "f3", MinMonsters: 1, Radius: 2, CooldownMS: 2000},
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirm := 150 * time.Millisecond
	// Only 2 monsters present: rule 1 (needs 4) matches nothing, rule 2
	// (needs 1) is feasible and must fire, even though it is second in line.
	crowd := fight.Crowd{Distances: []float64{1, 1.5}}
	at := confirmedCrowd(e, crowd, fullMana(), false, true, base, confirm)
	d := e.Decide(crowd, fullMana(), false, true, at, confirm)
	if !d.Fire || d.Hotkey != "f3" {
		t.Fatalf("oczekiwano f3 (pierwsza wykonalna), dostałem %+v", d)
	}
}

func TestEngineCooldownSharedByHotkeyAcrossRules(t *testing.T) {
	e := fight.NewEngine()
	e.SetRules([]fight.Rule{
		{Enabled: true, Hotkey: "f2", MinMonsters: 4, Radius: 1, CooldownMS: 2000},
		{Enabled: true, Hotkey: "f2", MinMonsters: 1, Radius: 2, CooldownMS: 2000},
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirm := 150 * time.Millisecond
	crowd := fight.Crowd{Distances: []float64{0.5, 0.5, 0.5, 0.5}}
	at := confirmedCrowd(e, crowd, fullMana(), false, true, base, confirm)
	d := e.Decide(crowd, fullMana(), false, true, at, confirm)
	if !d.Fire || d.Hotkey != "f2" {
		t.Fatalf("pierwsza reguła f2 powinna polecieć, dostałem %+v", d)
	}
	e.Emitted("f2", at)
	// Immediately after, rule 1's own cooldown blocks it - but rule 2 shares
	// the SAME hotkey's cooldown, so it must not fire either, even though its
	// own crowd count (1 needed, 4 present) would otherwise be feasible.
	d = e.Decide(crowd, fullMana(), false, true, at.Add(10*time.Millisecond), confirm)
	if d.Fire {
		t.Fatalf("cooldown klawisza f2 musi zablokować obie reguły, dostałem %+v", d)
	}
}

func TestEngineRequiresTarget(t *testing.T) {
	e := fight.NewEngine()
	e.SetRules([]fight.Rule{{Enabled: true, Hotkey: "f4", MinMonsters: 1, Radius: 2, CooldownMS: 1000, RequiresTarget: true}})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirm := 150 * time.Millisecond
	crowd := fight.Crowd{Distances: []float64{1}}
	at := confirmedCrowd(e, crowd, fullMana(), false, true, base, confirm)
	d := e.Decide(crowd, fullMana(), false, true, at, confirm)
	if d.Fire {
		t.Fatalf("reguła RequiresTarget bez celu nie może polecieć, dostałem %+v", d)
	}
	d = e.Decide(crowd, fullMana(), true, true, at, confirm)
	if !d.Fire {
		t.Fatalf("z celem ta sama reguła powinna polecieć, dostałem %+v", d)
	}
}

func TestEngineUnreadableManaBlocksOnlyRulesThatCostMana(t *testing.T) {
	e := fight.NewEngine()
	e.SetRules([]fight.Rule{
		{Enabled: true, Hotkey: "f5", MinMonsters: 1, Radius: 2, CooldownMS: 1000, MinManaPct: 50},
		{Enabled: true, Hotkey: "f6", MinMonsters: 1, Radius: 2, CooldownMS: 1000, MinManaPct: 0},
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirm := 150 * time.Millisecond
	crowd := fight.Crowd{Distances: []float64{1}}
	badMana := vitals.Reading{OK: false, Reason: "kalibracja"}
	at := confirmedCrowd(e, crowd, badMana, false, true, base, confirm)
	d := e.Decide(crowd, badMana, false, true, at, confirm)
	if !d.Fire || d.Hotkey != "f6" {
		t.Fatalf("reguła bez wymogu many musi polecieć mimo nieczytelnego odczytu, dostałem %+v", d)
	}
}

func TestEngineMixedCrowdBlocksOnlyAoE(t *testing.T) {
	e := fight.NewEngine()
	e.SetRules([]fight.Rule{
		{Enabled: true, Hotkey: "f7", MinMonsters: 3, Radius: 2, CooldownMS: 1000},
		{Enabled: true, Hotkey: "f8", MinMonsters: 1, Radius: 2, CooldownMS: 1000},
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirm := 150 * time.Millisecond
	crowd := fight.Crowd{Distances: []float64{0.5, 0.5, 0.5}, Mixed: true}
	at := confirmedCrowd(e, crowd, fullMana(), false, true, base, confirm)
	d := e.Decide(crowd, fullMana(), false, true, at, confirm)
	if !d.Fire || d.Hotkey != "f8" {
		t.Fatalf("mieszany tłum musi zablokować tylko regułę AoE (MinMonsters>1), dostałem %+v", d)
	}
}

func TestEngineWithinRoundsToNearestWholeTile(t *testing.T) {
	crowd := fight.Crowd{Distances: []float64{1.4, 1.5, 1.6}}
	if n := crowd.Within(1); n != 2 {
		t.Errorf("Within(1) = %d, oczekiwano 2 (próg 1,5)", n)
	}
}
