"""Shared game state.

A single GameState instance is passed to every module.  All reads and
writes go through this object so that modules stay decoupled from each
other while still being able to react to each other's observations.
"""

import threading
import time
from dataclasses import dataclass, field
from typing import Dict, List, Optional, Tuple


@dataclass
class Position:
    x: int = 0
    y: int = 0
    z: int = 0

    def distance_to(self, other: "Position") -> float:
        return ((self.x - other.x) ** 2 + (self.y - other.y) ** 2) ** 0.5

    def __eq__(self, other: object) -> bool:
        if not isinstance(other, Position):
            return False
        return self.x == other.x and self.y == other.y and self.z == other.z


class GameState:
    """Thread-safe shared state consumed by all bot modules."""

    def __init__(self) -> None:
        self._lock = threading.RLock()

        # ── resources ────────────────────────────────────────────────────────
        self.hp_percent: float = 100.0
        self.mana_percent: float = 100.0

        # ── position ─────────────────────────────────────────────────────────
        self.position: Position = Position()
        self.position_updated_at: float = 0.0
        # ring buffer: (timestamp, Position)
        self._position_history: List[Tuple[float, Position]] = []
        self._last_moved_at: float = time.monotonic()

        # ── combat ───────────────────────────────────────────────────────────
        self.enemy_in_battle_list: bool = False
        self.currently_attacking: bool = False
        self.attack_started_at: Optional[float] = None

        # area → expiry timestamp; cleared automatically when expired
        self._unreachable: Dict[Tuple[int, int, int], float] = {}

        # ── loot ─────────────────────────────────────────────────────────────
        # Set by CombatModule when an enemy dies; cleared by LootModule when done
        self.loot_pending: bool = False
        self.looting_active: bool = False

        # ── navigation ───────────────────────────────────────────────────────
        self.waypoint_index: int = 0

        # ── lifecycle ────────────────────────────────────────────────────────
        self.running: bool = True

    # ── position helpers ─────────────────────────────────────────────────────

    def update_position(self, pos: Position) -> None:
        with self._lock:
            if pos != self.position:
                self._last_moved_at = time.monotonic()
            self.position = pos
            self.position_updated_at = time.monotonic()
            self._position_history.append((time.monotonic(), pos))
            if len(self._position_history) > 20:
                self._position_history.pop(0)

    def position_stale(self, max_age: float = 3.0) -> bool:
        """True if the last position read is older than max_age seconds."""
        return (time.monotonic() - self.position_updated_at) > max_age

    def seconds_since_last_move(self) -> float:
        with self._lock:
            return time.monotonic() - self._last_moved_at

    # ── reachability helpers ─────────────────────────────────────────────────

    def mark_unreachable(self, pos: Position, duration: float = 30.0) -> None:
        """Temporarily blacklist an area so navigation skips it."""
        with self._lock:
            self._unreachable[(pos.x, pos.y, pos.z)] = time.monotonic() + duration

    def is_unreachable(self, pos: Position) -> bool:
        with self._lock:
            key = (pos.x, pos.y, pos.z)
            expiry = self._unreachable.get(key)
            if expiry is None:
                return False
            if time.monotonic() > expiry:
                del self._unreachable[key]
                return False
            return True
