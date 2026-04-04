package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFromYAML(t *testing.T) {
	yaml := `
capture:
  camera_index: 1
  minimap_region: [100, 200, 110, 110]
  health_bar_region: [10, 50, 200, 15]
  mana_bar_region: [10, 70, 200, 15]
  battle_list_region: [1650, 200, 180, 300]
minimap:
  data_path: "/tmp/maps"
  start_z: 8
waypoints:
  list:
    - x: 100
      "y": 200
      z: 7
      action: walk
  loop: false
healing:
  health_pot_key: "F1"
  health_threshold: 50
  health_cooldown: 1.5
  mana_pot_key: "F2"
  mana_threshold: 30
  mana_cooldown: 2.0
combat:
  attack_key: "F3"
  spell_keys: ["F4"]
  spell_cooldowns: [2.0]
movement:
  step_delay: 0.3
supplies:
  max_potions: 100
  refill_threshold: 10
  depot_waypoint_index: 0
web:
  host: "0.0.0.0"
  port: 9090
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Capture.CameraIndex != 1 {
		t.Errorf("CameraIndex = %d, want 1", cfg.Capture.CameraIndex)
	}
	if cfg.Minimap.StartZ != 8 {
		t.Errorf("StartZ = %d, want 8", cfg.Minimap.StartZ)
	}
	if len(cfg.Waypoints.List) != 1 {
		t.Fatalf("Waypoints.List len = %d, want 1", len(cfg.Waypoints.List))
	}
	if cfg.Waypoints.List[0].X != 100 {
		t.Errorf("Waypoint X = %d, want 100", cfg.Waypoints.List[0].X)
	}
	if cfg.Waypoints.Loop != false {
		t.Errorf("Loop = %v, want false", cfg.Waypoints.Loop)
	}
	if cfg.Healing.HealthThreshold != 50 {
		t.Errorf("HealthThreshold = %d, want 50", cfg.Healing.HealthThreshold)
	}
	if len(cfg.Combat.SpellKeys) != 1 || cfg.Combat.SpellKeys[0] != "F4" {
		t.Errorf("SpellKeys = %v, want [F4]", cfg.Combat.SpellKeys)
	}
	if cfg.Movement.StepDelay != 0.3 {
		t.Errorf("StepDelay = %f, want 0.3", cfg.Movement.StepDelay)
	}
	if cfg.Supplies.MaxPotions != 100 {
		t.Errorf("MaxPotions = %d, want 100", cfg.Supplies.MaxPotions)
	}
	if cfg.Web.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Web.Port)
	}
}

func TestLoadDefaultConfig(t *testing.T) {
	cfg, err := LoadConfig("../../configs/default.yaml")
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Capture.CameraIndex != 0 {
		t.Errorf("CameraIndex = %d, want 0", cfg.Capture.CameraIndex)
	}
	if cfg.Minimap.StartZ != 7 {
		t.Errorf("StartZ = %d, want 7", cfg.Minimap.StartZ)
	}
	if cfg.Waypoints.Loop != true {
		t.Errorf("Loop = %v, want true", cfg.Waypoints.Loop)
	}
}

func TestConfigToMap(t *testing.T) {
	cfg, err := LoadConfig("../../configs/default.yaml")
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	m := cfg.ToMap()
	capture, ok := m["capture"].(map[string]interface{})
	if !ok {
		t.Fatal("capture not a map")
	}
	// JSON numbers are float64
	if capture["camera_index"] != float64(0) {
		t.Errorf("camera_index = %v, want 0", capture["camera_index"])
	}
}
