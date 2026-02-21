"""Configuration dataclasses and YAML loader.

All tuneable parameters live in ``bot_config.yaml`` next to the project
root.  Sensible defaults are baked in so the bot can start even from a
minimal config file.
"""

import os
from dataclasses import dataclass, field
from typing import List, Optional, Tuple

try:
    import yaml
    _YAML_AVAILABLE = True
except ImportError:
    _YAML_AVAILABLE = False


# ── sub-configs ───────────────────────────────────────────────────────────────

@dataclass
class ScreenConfig:
    width: int = 2560
    height: int = 1440
    capture_fps: int = 20


@dataclass
class ViewportConfig:
    """Pixel geometry of the game world view (centred on the character)."""
    left: int = 0
    top: int = 0
    width: int = 1700
    height: int = 1280
    center_x: int = 1185    # screen x of the character tile centre
    center_y: int = 610     # screen y of the character tile centre
    tile_size: int = 64     # pixels per game tile at default zoom


@dataclass
class CoordDisplayConfig:
    """Screen region that shows the current X, Y, Z coordinates.

    In the default Tibia client layout this is the small text area below
    the minimap.  Run ``python calibrate.py --show-coords`` to locate it.
    """
    x: int = 1820
    y: int = 372
    width: int = 180
    height: int = 14


@dataclass
class HealingConfig:
    hp_threshold: float = 70.0        # press heal_key when HP < this %
    heal_key: str = "f1"
    mana_threshold: float = 30.0      # press mana_key when mana < this %
    mana_key: Optional[str] = None    # set to e.g. "f3"; None = disabled


@dataclass
class CombatConfig:
    monsters: List[str] = field(default_factory=lambda: ["swamp_troll"])
    attack_key: str = "space"
    stuck_timeout: float = 3.0        # seconds without position change = stuck
    unreachable_cooldown: float = 30.0  # seconds before retrying that area


@dataclass
class NavigationConfig:
    # List of (X, Y, Z) world coordinate tuples
    waypoints: List[Tuple[int, int, int]] = field(default_factory=list)
    waypoint_tolerance: int = 2       # tiles – how close = "arrived"
    move_interval: float = 0.9        # seconds between navigation clicks


@dataclass
class LootConfig:
    enabled: bool = True
    delay_after_kill: float = 1.5     # wait this long before looting
    templates_dir: str = "loot"       # directory holding item PNG templates
    # Item names (without .png) to pick up.  Empty list = take nothing.
    # Use ["*"] to take everything (legacy take-all mode).
    whitelist: List[str] = field(default_factory=list)


@dataclass
class BotConfig:
    screen: ScreenConfig = field(default_factory=ScreenConfig)
    viewport: ViewportConfig = field(default_factory=ViewportConfig)
    coord_display: CoordDisplayConfig = field(default_factory=CoordDisplayConfig)
    healing: HealingConfig = field(default_factory=HealingConfig)
    combat: CombatConfig = field(default_factory=CombatConfig)
    navigation: NavigationConfig = field(default_factory=NavigationConfig)
    loot: LootConfig = field(default_factory=LootConfig)


# ── loader ───────────────────────────────────────────────────────────────────

def load_config(path: str = "bot_config.yaml") -> BotConfig:
    """Load config from *path*, falling back to defaults for missing keys."""
    cfg = BotConfig()

    if not os.path.exists(path):
        print(f"[Config] {path} not found – using defaults")
        return cfg

    if not _YAML_AVAILABLE:
        print("[Config] PyYAML not installed – using defaults")
        return cfg

    with open(path) as f:
        raw = yaml.safe_load(f) or {}

    def _get(d: dict, *keys, default=None):
        for k in keys:
            if not isinstance(d, dict):
                return default
            d = d.get(k, default)
        return d

    s = raw.get("screen", {})
    cfg.screen = ScreenConfig(
        width=s.get("width", cfg.screen.width),
        height=s.get("height", cfg.screen.height),
        capture_fps=s.get("capture_fps", cfg.screen.capture_fps),
    )

    v = raw.get("viewport", {})
    cfg.viewport = ViewportConfig(
        left=v.get("left", cfg.viewport.left),
        top=v.get("top", cfg.viewport.top),
        width=v.get("width", cfg.viewport.width),
        height=v.get("height", cfg.viewport.height),
        center_x=v.get("center_x", cfg.viewport.center_x),
        center_y=v.get("center_y", cfg.viewport.center_y),
        tile_size=v.get("tile_size", cfg.viewport.tile_size),
    )

    cd = raw.get("coord_display", {})
    cfg.coord_display = CoordDisplayConfig(
        x=cd.get("x", cfg.coord_display.x),
        y=cd.get("y", cfg.coord_display.y),
        width=cd.get("width", cfg.coord_display.width),
        height=cd.get("height", cfg.coord_display.height),
    )

    h = raw.get("healing", {})
    cfg.healing = HealingConfig(
        hp_threshold=h.get("hp_threshold", cfg.healing.hp_threshold),
        heal_key=h.get("heal_key", cfg.healing.heal_key),
        mana_threshold=h.get("mana_threshold", cfg.healing.mana_threshold),
        mana_key=h.get("mana_key", cfg.healing.mana_key),
    )

    c = raw.get("combat", {})
    cfg.combat = CombatConfig(
        monsters=c.get("monsters", cfg.combat.monsters),
        attack_key=c.get("attack_key", cfg.combat.attack_key),
        stuck_timeout=c.get("stuck_timeout", cfg.combat.stuck_timeout),
        unreachable_cooldown=c.get("unreachable_cooldown", cfg.combat.unreachable_cooldown),
    )

    n = raw.get("navigation", {})
    raw_wps = n.get("waypoints", [])
    waypoints = [tuple(int(v) for v in wp) for wp in raw_wps]
    cfg.navigation = NavigationConfig(
        waypoints=waypoints,
        waypoint_tolerance=n.get("waypoint_tolerance", cfg.navigation.waypoint_tolerance),
        move_interval=n.get("move_interval", cfg.navigation.move_interval),
    )

    lo = raw.get("loot", {})
    cfg.loot = LootConfig(
        enabled=lo.get("enabled", cfg.loot.enabled),
        delay_after_kill=lo.get("delay_after_kill", cfg.loot.delay_after_kill),
        templates_dir=lo.get("templates_dir", cfg.loot.templates_dir),
        whitelist=lo.get("whitelist", cfg.loot.whitelist),
    )

    return cfg
