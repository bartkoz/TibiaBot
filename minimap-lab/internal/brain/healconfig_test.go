package brain

import (
	"strings"
	"testing"

	"minimap-lab/internal/heal"
)

func rule() heal.Rule {
	return heal.Rule{Enabled: true, Resource: heal.ResourceHP, BelowPct: 60,
		Hotkey: "f1", CooldownMS: 1000}
}

func TestHealConfigRejectsBadRules(t *testing.T) {
	cases := []struct {
		name string
		edit func(*heal.Rule)
		want string
	}{
		{"nieznany zasób", func(r *heal.Rule) { r.Resource = "stamina" }, "hp albo mana"},
		{"próg zerowy", func(r *heal.Rule) { r.BelowPct = 0 }, "1–99"},
		{"próg pełny", func(r *heal.Rule) { r.BelowPct = 100 }, "1–99"},
		{"mana ujemna", func(r *heal.Rule) { r.MinManaPct = -1 }, "0–99"},
		{"cooldown za krótki", func(r *heal.Rule) { r.CooldownMS = 99 }, "100–60000"},
		{"cooldown za długi", func(r *heal.Rule) { r.CooldownMS = 60001 }, "100–60000"},
		{"klawisz nie istnieje", func(r *heal.Rule) { r.Hotkey = "księżyc" }, "klawisz"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := rule()
			c.edit(&r)
			err := HealConfig{Enabled: true, Rules: []heal.Rule{r}}.validate()
			if err == nil {
				t.Fatal("konfiguracja przeszła, a nie powinna")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("powód = %q, oczekiwano wzmianki o %q", err.Error(), c.want)
			}
		})
	}
}

func TestHealConfigBoundsTheRuleCount(t *testing.T) {
	rules := make([]heal.Rule, maxHealRules+1)
	for i := range rules {
		rules[i] = rule()
	}
	if err := (HealConfig{Enabled: true, Rules: rules}).validate(); err == nil {
		t.Fatal("dziewiąta reguła przeszła")
	}
}

// An empty list is a legal way to say "do not heal", and a disabled rule is a
// legal way to park one line without deleting it.
func TestHealConfigAcceptsAnEmptyList(t *testing.T) {
	if err := (HealConfig{}).validate(); err != nil {
		t.Fatalf("pusta lista odrzucona: %v", err)
	}
}

// A disabled rule is still checked: the panel would otherwise let a broken
// line sit there until the day it is switched on.
func TestHealConfigChecksDisabledRulesToo(t *testing.T) {
	r := rule()
	r.Enabled, r.Hotkey = false, "księżyc"
	if err := (HealConfig{Rules: []heal.Rule{r}}).validate(); err == nil {
		t.Fatal("wyłączona reguła z bzdurnym klawiszem przeszła")
	}
}

func TestConfigCarriesHealRules(t *testing.T) {
	c := baseConfig()
	c.Heal = HealConfig{Enabled: true, Rules: []heal.Rule{rule()}}
	if err := c.validate(); err != nil {
		t.Fatalf("dobra konfiguracja odrzucona: %v", err)
	}
	c.Heal.Rules[0].Hotkey = "księżyc"
	if err := c.validate(); err == nil {
		t.Fatal("zła reguła przeszła walidacją całej konfiguracji")
	}
}

// Escape is reserved: it means exactly one thing to the client, and a heal
// rule bound to it would cancel the target every time the character drank.
func TestHealConfigRefusesTheReservedEscapeKey(t *testing.T) {
	r := rule()
	r.Hotkey = "escape"
	err := (HealConfig{Enabled: true, Rules: []heal.Rule{r}}).validate()
	if err == nil {
		t.Fatal("reguła leczenia na zastrzeżonym klawiszu przeszła")
	}
	if !strings.Contains(err.Error(), "zastrzeżony") {
		t.Fatalf("powód = %q", err)
	}
}
