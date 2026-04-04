"""YAML configuration loader with typed dataclasses."""

from __future__ import annotations

import dataclasses
from dataclasses import dataclass, field
from pathlib import Path

import yaml


@dataclass
class CaptureConfig:
    camera_index: int = 0
    minimap_region: list[int] = field(default_factory=lambda: [1650, 20, 110, 110])
    health_bar_region: list[int] = field(default_factory=lambda: [10, 50, 200, 15])
    mana_bar_region: list[int] = field(default_factory=lambda: [10, 70, 200, 15])
    battle_list_region: list[int] = field(default_factory=lambda: [1650, 200, 180, 300])


@dataclass
class MinimapConfig:
    data_path: str = "./minimap"
    start_z: int = 7


@dataclass
class WaypointEntry:
    x: int = 0
    y: int = 0
    z: int = 7
    action: str = "walk"


@dataclass
class WaypointsConfig:
    list: list[WaypointEntry] = field(default_factory=list)
    loop: bool = True


@dataclass
class HealingConfig:
    health_pot_key: str = "F1"
    health_threshold: int = 60
    health_cooldown: float = 1.0
    mana_pot_key: str = "F2"
    mana_threshold: int = 40
    mana_cooldown: float = 1.0


@dataclass
class CombatConfig:
    attack_key: str = "F3"
    spell_keys: list[str] = field(default_factory=lambda: ["F4", "F5"])
    spell_cooldowns: list[float] = field(default_factory=lambda: [2.0, 6.0])


@dataclass
class MovementConfig:
    step_delay: float = 0.2


@dataclass
class SuppliesConfig:
    max_potions: int = 200
    refill_threshold: int = 20
    depot_waypoint_index: int = 0


@dataclass
class WebConfig:
    host: str = "127.0.0.1"
    port: int = 8080


@dataclass
class BotConfig:
    capture: CaptureConfig = field(default_factory=CaptureConfig)
    minimap: MinimapConfig = field(default_factory=MinimapConfig)
    waypoints: WaypointsConfig = field(default_factory=WaypointsConfig)
    healing: HealingConfig = field(default_factory=HealingConfig)
    combat: CombatConfig = field(default_factory=CombatConfig)
    movement: MovementConfig = field(default_factory=MovementConfig)
    supplies: SuppliesConfig = field(default_factory=SuppliesConfig)
    web: WebConfig = field(default_factory=WebConfig)

    def to_dict(self) -> dict:
        return dataclasses.asdict(self)


def _build_dataclass(cls, data: dict):
    """Recursively build a dataclass from a dict."""
    if data is None:
        return cls()
    fieldtypes = {f.name: f.type for f in dataclasses.fields(cls)}
    kwargs = {}
    for key, value in data.items():
        if key in fieldtypes:
            ft = fieldtypes[key]
            if ft == "list[WaypointEntry]" and isinstance(value, list):
                kwargs[key] = [WaypointEntry(**entry) for entry in value]
            else:
                kwargs[key] = value
    return cls(**kwargs)


def load_config(path: Path) -> BotConfig:
    """Load a BotConfig from a YAML file."""
    with open(path) as f:
        raw = yaml.safe_load(f)
    if raw is None:
        return BotConfig()
    return BotConfig(
        capture=_build_dataclass(CaptureConfig, raw.get("capture")),
        minimap=_build_dataclass(MinimapConfig, raw.get("minimap")),
        waypoints=_build_dataclass(WaypointsConfig, raw.get("waypoints")),
        healing=_build_dataclass(HealingConfig, raw.get("healing")),
        combat=_build_dataclass(CombatConfig, raw.get("combat")),
        movement=_build_dataclass(MovementConfig, raw.get("movement")),
        supplies=_build_dataclass(SuppliesConfig, raw.get("supplies")),
        web=_build_dataclass(WebConfig, raw.get("web")),
    )
