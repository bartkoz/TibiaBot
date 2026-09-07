package heal

import (
	"testing"
	"time"

	"minimap-lab/internal/vitals"
)

var base = time.Unix(1_700_000_000, 0)

func ok(pct float64) vitals.Reading { return vitals.Reading{Percent: pct / 100, OK: true} }
func bad(why string) vitals.Reading { return vitals.Reading{Reason: why} }

func potion(below float64, key string) Rule {
	return Rule{Enabled: true, Resource: ResourceHP, BelowPct: below, Hotkey: key, CooldownMS: 1000}
}

func TestFirstFeasibleRuleWins(t *testing.T) {
	cases := []struct {
		name     string
		rules    []Rule
		hp, mana vitals.Reading
		want     string // hotkey albo "" gdy nic
	}{
		{
			name:  "pierwsza pasująca od góry",
			rules: []Rule{potion(80, "f1"), potion(50, "f2")},
			hp:    ok(40), mana: ok(100),
			want: "f1",
		},
		{
			name:  "próg działa jako mniejsze bądź równe",
			rules: []Rule{potion(50, "f1")},
			hp:    ok(50), mana: ok(100),
			want: "f1",
		},
		{
			name:  "powyżej progu nic nie leci",
			rules: []Rule{potion(50, "f1")},
			hp:    ok(51), mana: ok(100),
			want: "",
		},
		{
			name:  "wyłączona reguła jest pomijana",
			rules: []Rule{{Resource: ResourceHP, BelowPct: 80, Hotkey: "f1", CooldownMS: 1000}, potion(80, "f2")},
			hp:    ok(40), mana: ok(100),
			want: "f2",
		},
		{
			name: "reguła many czyta pasek many",
			rules: []Rule{
				{Enabled: true, Resource: ResourceMana, BelowPct: 40, Hotkey: "f3", CooldownMS: 1000},
			},
			hp: ok(100), mana: ok(30),
			want: "f3",
		},
		{
			name: "czar bez many ustępuje potionowi niżej na liście",
			rules: []Rule{
				{Enabled: true, Resource: ResourceHP, BelowPct: 80, Hotkey: "f4", CooldownMS: 1000, MinManaPct: 50},
				potion(80, "f1"),
			},
			hp: ok(40), mana: ok(10),
			want: "f1",
		},
		{
			name:  "nieufny odczyt HP nie leczy",
			rules: []Rule{potion(80, "f1")},
			hp:    bad("kalibracja się rozjechała"), mana: ok(100),
			want: "",
		},
		{
			name:  "HP na zerze nie leczy",
			rules: []Rule{potion(80, "f1")},
			hp:    ok(0), mana: ok(100),
			want: "",
		},
		{
			name:  "pusta lista nie leczy",
			rules: nil,
			hp:    ok(1), mana: ok(1),
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := NewEngine()
			e.SetRules(c.rules)
			got := e.Decide(c.hp, c.mana, base)
			if c.want == "" {
				if got.Fire {
					t.Fatalf("zapaliła się reguła %d (%s), oczekiwano ciszy", got.Index, got.Hotkey)
				}
				if got.Reason == "" {
					t.Error("cisza bez powodu — panel nie ma czego pokazać")
				}
				return
			}
			if !got.Fire || got.Hotkey != c.want {
				t.Fatalf("got %+v, oczekiwano klawisza %s", got, c.want)
			}
		})
	}
}

// A rule with no mana requirement must not be blocked by an unreadable mana
// bar: that would stop health potions exactly when the calibration slipped.
func TestHealthRuleDoesNotNeedTheManaBar(t *testing.T) {
	e := NewEngine()
	e.SetRules([]Rule{potion(80, "f1")})
	if got := e.Decide(ok(40), bad("pasek many nieczytelny"), base); !got.Fire {
		t.Fatalf("got %+v", got)
	}
}

func TestSpellRuleNeedsAReadableManaBar(t *testing.T) {
	e := NewEngine()
	e.SetRules([]Rule{{Enabled: true, Resource: ResourceHP, BelowPct: 80, Hotkey: "f4",
		CooldownMS: 1000, MinManaPct: 20}})
	got := e.Decide(ok(40), bad("pasek many nieczytelny"), base)
	if got.Fire {
		t.Fatalf("czar poleciał bez wiarygodnej many: %+v", got)
	}
}

// The cooldown belongs to the key, not the rule: two rules on the same bottle
// are two thresholds for one exhaust.
func TestCooldownIsSharedByRulesOnTheSameKey(t *testing.T) {
	e := NewEngine()
	e.SetRules([]Rule{potion(80, "f1"), potion(40, "f1")})
	first := e.Decide(ok(30), ok(100), base)
	if !first.Fire {
		t.Fatalf("got %+v", first)
	}
	e.Emitted(first.Hotkey, base)
	if got := e.Decide(ok(30), ok(100), base.Add(900*time.Millisecond)); got.Fire {
		t.Fatalf("druga reguła obeszła cooldown klawisza: %+v", got)
	}
	if got := e.Decide(ok(30), ok(100), base.Add(1100*time.Millisecond)); !got.Fire {
		t.Fatalf("po cooldownie nic nie poleciało: %+v", got)
	}
}

// Decide must not remember anything: a decision the driver refused cannot burn
// a cooldown, or a refused tap would silence the rule for a second.
func TestOnlyAnEmissionStartsTheCooldown(t *testing.T) {
	e := NewEngine()
	e.SetRules([]Rule{potion(80, "f1")})
	if got := e.Decide(ok(30), ok(100), base); !got.Fire {
		t.Fatalf("got %+v", got)
	}
	if got := e.Decide(ok(30), ok(100), base.Add(10*time.Millisecond)); !got.Fire {
		t.Fatalf("sama decyzja wypaliła cooldown: %+v", got)
	}
}

// The gap is measured against when the frame was captured, not when the
// decision runs: the client's bar needs time to show the first sip, and an
// observation taken before it did is no evidence at all.
func TestSharedGapIsMeasuredFromTheCapture(t *testing.T) {
	e := NewEngine()
	e.SetRules([]Rule{potion(80, "f1"), {Enabled: true, Resource: ResourceHP,
		BelowPct: 80, Hotkey: "f2", CooldownMS: 100}})
	e.Emitted("f1", base)
	if got := e.Decide(ok(30), ok(100), base.Add(MinGap-time.Millisecond)); got.Fire {
		t.Fatalf("druga butelka poszła w odstępie: %+v", got)
	}
	if got := e.Decide(ok(30), ok(100), base.Add(MinGap)); !got.Fire || got.Hotkey != "f2" {
		t.Fatalf("po odstępie: %+v", got)
	}
}
