import tempfile
from pathlib import Path

import yaml

from cavebot.core.config import (
    BotConfig,
    CaptureConfig,
    CombatConfig,
    HealingConfig,
    MinimapConfig,
    MovementConfig,
    SuppliesConfig,
    WaypointEntry,
    WaypointsConfig,
    WebConfig,
    load_config,
)


def test_load_config_from_yaml(tmp_path: Path):
    cfg_data = {
        "capture": {
            "camera_index": 1,
            "minimap_region": [100, 200, 110, 110],
            "health_bar_region": [10, 50, 200, 15],
            "mana_bar_region": [10, 70, 200, 15],
            "battle_list_region": [1650, 200, 180, 300],
        },
        "minimap": {"data_path": "/tmp/maps", "start_z": 8},
        "waypoints": {
            "list": [{"x": 100, "y": 200, "z": 7, "action": "walk"}],
            "loop": False,
        },
        "healing": {
            "health_pot_key": "F1",
            "health_threshold": 50,
            "health_cooldown": 1.5,
            "mana_pot_key": "F2",
            "mana_threshold": 30,
            "mana_cooldown": 2.0,
        },
        "combat": {
            "attack_key": "F3",
            "spell_keys": ["F4"],
            "spell_cooldowns": [2.0],
        },
        "movement": {"step_delay": 0.3},
        "supplies": {
            "max_potions": 100,
            "refill_threshold": 10,
            "depot_waypoint_index": 0,
        },
        "web": {"host": "0.0.0.0", "port": 9090},
    }
    cfg_path = tmp_path / "config.yaml"
    cfg_path.write_text(yaml.dump(cfg_data))
    config = load_config(cfg_path)
    assert isinstance(config, BotConfig)
    assert config.capture.camera_index == 1
    assert config.minimap.start_z == 8
    assert len(config.waypoints.list) == 1
    assert config.waypoints.list[0].x == 100
    assert config.waypoints.loop is False
    assert config.healing.health_threshold == 50
    assert config.combat.spell_keys == ["F4"]
    assert config.movement.step_delay == 0.3
    assert config.supplies.max_potions == 100
    assert config.web.port == 9090


def test_load_default_config():
    config = load_config(Path("configs/default.yaml"))
    assert config.capture.camera_index == 0
    assert config.minimap.start_z == 7
    assert config.waypoints.loop is True


def test_config_to_dict():
    config = load_config(Path("configs/default.yaml"))
    d = config.to_dict()
    assert d["capture"]["camera_index"] == 0
    assert d["minimap"]["data_path"] == "./minimap"
