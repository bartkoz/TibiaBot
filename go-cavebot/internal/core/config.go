package core

import (
	"encoding/json"
	"os"

	"gopkg.in/yaml.v3"
)

type Region [4]int

type CaptureConfig struct {
	CameraIndex      int    `yaml:"camera_index" json:"camera_index"`
	MinimapRegion    Region `yaml:"minimap_region" json:"minimap_region"`
	HealthBarRegion  Region `yaml:"health_bar_region" json:"health_bar_region"`
	ManaBarRegion    Region `yaml:"mana_bar_region" json:"mana_bar_region"`
	BattleListRegion Region `yaml:"battle_list_region" json:"battle_list_region"`
}

type MinimapConfig struct {
	DataPath string `yaml:"data_path" json:"data_path"`
	StartZ   int    `yaml:"start_z" json:"start_z"`
}

type WaypointEntry struct {
	X      int    `yaml:"x" json:"x"`
	Y      int    `yaml:"y" json:"y"`
	Z      int    `yaml:"z" json:"z"`
	Action string `yaml:"action" json:"action"`
}

type WaypointsConfig struct {
	List []WaypointEntry `yaml:"list" json:"list"`
	Loop bool            `yaml:"loop" json:"loop"`
}

type HealingConfig struct {
	HealthPotKey    string  `yaml:"health_pot_key" json:"health_pot_key"`
	HealthThreshold int     `yaml:"health_threshold" json:"health_threshold"`
	HealthCooldown  float64 `yaml:"health_cooldown" json:"health_cooldown"`
	ManaPotKey      string  `yaml:"mana_pot_key" json:"mana_pot_key"`
	ManaThreshold   int     `yaml:"mana_threshold" json:"mana_threshold"`
	ManaCooldown    float64 `yaml:"mana_cooldown" json:"mana_cooldown"`
}

type CombatConfig struct {
	AttackKey      string    `yaml:"attack_key" json:"attack_key"`
	SpellKeys      []string  `yaml:"spell_keys" json:"spell_keys"`
	SpellCooldowns []float64 `yaml:"spell_cooldowns" json:"spell_cooldowns"`
}

type MovementConfig struct {
	StepDelay float64 `yaml:"step_delay" json:"step_delay"`
}

type SuppliesConfig struct {
	MaxPotions         int `yaml:"max_potions" json:"max_potions"`
	RefillThreshold    int `yaml:"refill_threshold" json:"refill_threshold"`
	DepotWaypointIndex int `yaml:"depot_waypoint_index" json:"depot_waypoint_index"`
}

type WebConfig struct {
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
}

type BotConfig struct {
	Capture   CaptureConfig   `yaml:"capture" json:"capture"`
	Minimap   MinimapConfig   `yaml:"minimap" json:"minimap"`
	Waypoints WaypointsConfig `yaml:"waypoints" json:"waypoints"`
	Healing   HealingConfig   `yaml:"healing" json:"healing"`
	Combat    CombatConfig    `yaml:"combat" json:"combat"`
	Movement  MovementConfig  `yaml:"movement" json:"movement"`
	Supplies  SuppliesConfig  `yaml:"supplies" json:"supplies"`
	Web       WebConfig       `yaml:"web" json:"web"`
}

func DefaultConfig() BotConfig {
	return BotConfig{
		Capture: CaptureConfig{
			CameraIndex:      0,
			MinimapRegion:    Region{1650, 20, 110, 110},
			HealthBarRegion:  Region{10, 50, 200, 15},
			ManaBarRegion:    Region{10, 70, 200, 15},
			BattleListRegion: Region{1650, 200, 180, 300},
		},
		Minimap: MinimapConfig{
			DataPath: "./minimap",
			StartZ:   7,
		},
		Waypoints: WaypointsConfig{
			Loop: true,
		},
		Healing: HealingConfig{
			HealthPotKey:    "F1",
			HealthThreshold: 60,
			HealthCooldown:  1.0,
			ManaPotKey:      "F2",
			ManaThreshold:   40,
			ManaCooldown:    1.0,
		},
		Combat: CombatConfig{
			AttackKey:      "F3",
			SpellKeys:      []string{"F4", "F5"},
			SpellCooldowns: []float64{2.0, 6.0},
		},
		Movement: MovementConfig{
			StepDelay: 0.2,
		},
		Supplies: SuppliesConfig{
			MaxPotions:         200,
			RefillThreshold:    20,
			DepotWaypointIndex: 0,
		},
		Web: WebConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
	}
}

func LoadConfig(path string) (*BotConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *BotConfig) ToMap() map[string]interface{} {
	data, err := json.Marshal(c)
	if err != nil {
		return map[string]interface{}{}
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]interface{}{}
	}
	return m
}
