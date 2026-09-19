package brain

import (
	"strings"
	"testing"

	"minimap-lab/internal/fight"
)

// calibratedFight is a whole, valid fight configuration, the way calibrated()
// is for CombatConfig: enough to pass validate() as-is.
func calibratedFight() FightConfig {
	return FightConfig{Enabled: true, AttackKey: "space"}
}

func TestFightConfigAcceptsCalibrated(t *testing.T) {
	if err := calibratedFight().withDefaults().validate(4); err != nil {
		t.Fatalf("poprawna konfiguracja odrzucona: %v", err)
	}
}

func TestFightConfigDefaults(t *testing.T) {
	c := calibratedFight().withDefaults()
	if c.ConfirmMS != 150 || c.LeaveFightMS != 600 || c.TargetRetryMS != 600 ||
		c.TargetAttempts != 3 || c.TargetBackoffMS != 2000 || c.TargetStallMS != 15000 ||
		c.FightPauseMS != 10000 {
		t.Errorf("czasy domyślne nie zostały wypełnione: %+v", c)
	}
	if c.BlockMixedCrowd == nil || !*c.BlockMixedCrowd {
		t.Errorf("block_mixed_crowd musi domyślnie być true dla brakującego klucza, dostałem %+v", c.BlockMixedCrowd)
	}
}

func TestFightConfigBlockMixedCrowdFalseIsRespected(t *testing.T) {
	c := calibratedFight()
	f := false
	c.BlockMixedCrowd = &f
	c = c.withDefaults()
	if c.BlockMixedCrowd == nil || *c.BlockMixedCrowd {
		t.Error("jawne false nie może zostać zamienione na domyślne true")
	}
}

func TestFightConfigDisabledIsLegalWithoutAttackKey(t *testing.T) {
	if err := (FightConfig{}).withDefaults().validate(4); err != nil {
		t.Fatalf("wyłączona, pusta konfiguracja musi być dozwolona: %v", err)
	}
}

func TestFightConfigEnabledRequiresAttackKey(t *testing.T) {
	c := FightConfig{Enabled: true}
	err := c.withDefaults().validate(4)
	if err == nil || !strings.Contains(err.Error(), "attack_key") {
		t.Fatalf("enabled bez attack_key musi być odrzucone, dostałem: %v", err)
	}
}

func TestFightConfigRejections(t *testing.T) {
	// Every edit has to survive withDefaults(), which the table body runs
	// before validate(): a field set back to zero is read as "the panel left
	// it out" and promoted to its default, so an out-of-range case must pick
	// a non-zero value or it would prove nothing.
	tests := []struct {
		name string
		edit func(*FightConfig)
		want string
	}{
		{"confirm_ms poza zakresem", func(c *FightConfig) { c.ConfirmMS = 10000 }, "confirm_ms"},
		{"leave_fight_ms poza zakresem", func(c *FightConfig) { c.LeaveFightMS = 100 }, "leave_fight_ms"},
		{"target_retry_ms poza zakresem", func(c *FightConfig) { c.TargetRetryMS = 10 }, "target_retry_ms"},
		{"target_attempts poza zakresem", func(c *FightConfig) { c.TargetAttempts = 11 }, "target_attempts"},
		{"target_backoff_ms poza zakresem", func(c *FightConfig) { c.TargetBackoffMS = 100 }, "target_backoff_ms"},
		{"target_stall_ms poza zakresem", func(c *FightConfig) { c.TargetStallMS = 100 }, "target_stall_ms"},
		{"fight_pause_ms poza zakresem", func(c *FightConfig) { c.FightPauseMS = 100 }, "fight_pause_ms"},
		{"nieznany klawisz ataku", func(c *FightConfig) { c.AttackKey = "??" }, "attack_key"},
		{"za dużo reguł", func(c *FightConfig) {
			for i := 0; i < 9; i++ {
				c.Spells = append(c.Spells, fight.Rule{Hotkey: "f1", CooldownMS: 1000})
			}
		}, "reguł"},
		{"reguła: nieznany klawisz", func(c *FightConfig) {
			c.Spells = []fight.Rule{{Hotkey: "??", CooldownMS: 1000}}
		}, "klawisz"},
		{"reguła: min_monsters poza zakresem", func(c *FightConfig) {
			c.Spells = []fight.Rule{{Hotkey: "f2", CooldownMS: 1000, MinMonsters: 65}}
		}, "min_monsters"},
		{"reguła: promień większy niż promień decyzji", func(c *FightConfig) {
			c.Spells = []fight.Rule{{Hotkey: "f2", CooldownMS: 1000, MinMonsters: 1, Radius: 9}}
		}, "promień"},
		{"reguła: cooldown poza zakresem", func(c *FightConfig) {
			c.Spells = []fight.Rule{{Hotkey: "f2", CooldownMS: 50, MinMonsters: 1, Radius: 1}}
		}, "cooldown"},
		{"reguła: min_mana_pct poza zakresem", func(c *FightConfig) {
			c.Spells = []fight.Rule{{Hotkey: "f2", CooldownMS: 1000, MinMonsters: 1, Radius: 1, MinManaPct: 150}}
		}, "mana"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := calibratedFight()
			tt.edit(&c)
			err := c.withDefaults().validate(4)
			if err == nil {
				t.Fatal("konfiguracja przeszła, choć nie powinna")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("komunikat %q nie zawiera %q", err.Error(), tt.want)
			}
		})
	}
}

func TestFightConfigDisabledRulesAreStillValidated(t *testing.T) {
	c := calibratedFight()
	c.Spells = []fight.Rule{{Enabled: false, Hotkey: "??", CooldownMS: 1000, MinMonsters: 1, Radius: 1}}
	err := c.withDefaults().validate(4)
	if err == nil {
		t.Fatal("wyłączona reguła z niepoprawnym klawiszem musi i tak zostać odrzucona")
	}
}

func TestFightConfigRuleDefaults(t *testing.T) {
	c := calibratedFight()
	c.Spells = []fight.Rule{{Hotkey: "f2", MinMonsters: 1, Radius: 1}}
	c = c.withDefaults()
	if c.Spells[0].CooldownMS != 2000 {
		t.Errorf("domyślny cooldown reguły = %d, oczekiwano 2000", c.Spells[0].CooldownMS)
	}
}
