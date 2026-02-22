"""Configuration dataclasses and YAML loader.

All tuneable parameters live in ``bot_config.yaml`` next to the project
root.  Sensible defaults are baked in so the bot can start even from a
minimal config file.
"""

import json
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
    attack_key: str = "space"
    stuck_timeout: float = 3.0        # seconds without position change = stuck
    unreachable_cooldown: float = 30.0  # seconds before retrying that area
    # Row and column offset from the enemy-detection pixel (_battle_pixel) to
    # the top-left corner of the battle list entry.  The red attack border
    # lives there when a monster is selected.  Default (-10, -10) is derived
    # from the standard Tibia client where each battle entry is 20×20 px.
    attack_indicator_offset: Tuple[int, int] = (-10, -10)


@dataclass
class NavigationConfig:
    enabled: bool = True              # set to false to disable waypoint walking
    # Path to a JSON waypoints file produced by record_waypoints.py.
    # Takes priority over the inline `waypoints` list below.
    waypoints_file: Optional[str] = None
    # Inline fallback – ignored when waypoints_file is set.
    waypoints: List[Tuple[int, int, int]] = field(default_factory=list)
    waypoint_tolerance: int = 2       # tiles – how close = "arrived"
    move_interval: float = 0.9        # seconds between navigation clicks


def load_waypoints_json(path: str) -> List[Tuple[int, int, int]]:
    """Load waypoints from a JSON file created by record_waypoints.py."""
    with open(path) as f:
        data = json.load(f)
    # Support both {"waypoints": [[x,y,z], ...]} and bare [[x,y,z], ...]
    raw = data.get("waypoints", data) if isinstance(data, dict) else data
    return [tuple(int(v) for v in wp) for wp in raw]


@dataclass
class MinimapConfig:
    """Screen region of the minimap + navigation parameters for visual odometry."""
    enabled: bool = False
    # Pixel bounds of the minimap window on screen.
    # Run:  python calibrate.py --show-minimap  to locate these.
    x: int = 1633
    y: int = 44
    width: int = 106
    height: int = 109
    # Size of the centre crop saved as each waypoint template (must be < width/height).
    template_size: int = 40
    # Pixel distance from minimap centre that counts as "arrived" at a waypoint.
    arrival_px: int = 8
    # Seconds between navigation clicks.
    move_interval: float = 0.9
    # Seconds without minimap movement before declaring "stuck" and advancing.
    stuck_timeout: float = 5.0
    # JSON file produced by record_minimap_waypoints.py.
    waypoints_file: Optional[str] = None


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
    minimap: MinimapConfig = field(default_factory=MinimapConfig)
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
    raw_offset = c.get("attack_indicator_offset", None)
    attack_offset = tuple(raw_offset) if raw_offset else cfg.combat.attack_indicator_offset
    cfg.combat = CombatConfig(
        attack_key=c.get("attack_key", cfg.combat.attack_key),
        stuck_timeout=c.get("stuck_timeout", cfg.combat.stuck_timeout),
        unreachable_cooldown=c.get("unreachable_cooldown", cfg.combat.unreachable_cooldown),
        attack_indicator_offset=attack_offset,
    )

    n = raw.get("navigation", {})
    nav_enabled = n.get("enabled", cfg.navigation.enabled)
    wps_file = n.get("waypoints_file", None)
    if wps_file:
        try:
            waypoints = load_waypoints_json(wps_file)
            print(f"[Config] Loaded {len(waypoints)} waypoints from {wps_file}")
        except Exception as e:
            print(f"[Config] Could not load waypoints_file {wps_file!r}: {e}")
            waypoints = []
    else:
        raw_wps = n.get("waypoints", [])
        waypoints = [tuple(int(v) for v in wp) for wp in raw_wps]
    cfg.navigation = NavigationConfig(
        enabled=nav_enabled,
        waypoints_file=wps_file,
        waypoints=waypoints,
        waypoint_tolerance=n.get("waypoint_tolerance", cfg.navigation.waypoint_tolerance),
        move_interval=n.get("move_interval", cfg.navigation.move_interval),
    )

    mm = raw.get("minimap", {})
    cfg.minimap = MinimapConfig(
        enabled=mm.get("enabled", cfg.minimap.enabled),
        x=mm.get("x", cfg.minimap.x),
        y=mm.get("y", cfg.minimap.y),
        width=mm.get("width", cfg.minimap.width),
        height=mm.get("height", cfg.minimap.height),
        template_size=mm.get("template_size", cfg.minimap.template_size),
        arrival_px=mm.get("arrival_px", cfg.minimap.arrival_px),
        move_interval=mm.get("move_interval", cfg.minimap.move_interval),
        stuck_timeout=mm.get("stuck_timeout", cfg.minimap.stuck_timeout),
        waypoints_file=mm.get("waypoints_file", cfg.minimap.waypoints_file),
    )

    lo = raw.get("loot", {})
    cfg.loot = LootConfig(
        enabled=lo.get("enabled", cfg.loot.enabled),
        delay_after_kill=lo.get("delay_after_kill", cfg.loot.delay_after_kill),
        templates_dir=lo.get("templates_dir", cfg.loot.templates_dir),
        whitelist=lo.get("whitelist", cfg.loot.whitelist),
    )

    return cfg
