"""Main bot state machine."""

from __future__ import annotations

import enum
import logging
import threading
import time

import numpy as np

from cavebot.combat.fighter import SpellRotation, attack
from cavebot.combat.targeting import TargetingSystem
from cavebot.core.config import BotConfig
from cavebot.core.input import press_key, press_keys_simultaneous
from cavebot.navigation.atlas import Atlas
from cavebot.navigation.pathfinder import find_path
from cavebot.survival.healer import Healer
from cavebot.survival.supplies import SupplyTracker
from cavebot.vision.capture import FrameCapture
from cavebot.vision.health import read_bar_percentage
from cavebot.vision.minimap import MinimapLocator

logger = logging.getLogger(__name__)


class BotState(enum.Enum):
    IDLE = "IDLE"
    WALKING = "WALKING"
    COMBAT = "COMBAT"
    LOOTING = "LOOTING"
    HEALING = "HEALING"
    REFILL = "REFILL"


class BotStateMachine:
    def __init__(self):
        self._state = BotState.IDLE
        self._running = False
        self.position: tuple[int, int, int] = (0, 0, 7)
        self.health_pct: float = 100.0
        self.mana_pct: float = 100.0
        self.kills: int = 0
        self.waypoint_index: int = 0
        self.events: list[dict] = []

    @property
    def state(self) -> BotState:
        return self._state

    @property
    def running(self) -> bool:
        return self._running

    def start(self) -> None:
        self._running = True
        self._state = BotState.WALKING
        self._log_event("Bot started")

    def stop(self) -> None:
        self._running = False
        self._state = BotState.IDLE
        self._log_event("Bot stopped")

    def transition(self, new_state: BotState) -> None:
        old = self._state
        self._state = new_state
        self._log_event(f"State: {old.value} -> {new_state.value}")

    def status(self) -> dict:
        return {
            "state": self._state.value,
            "running": self._running,
            "position": self.position,
            "health_pct": self.health_pct,
            "mana_pct": self.mana_pct,
            "kills": self.kills,
            "waypoint_index": self.waypoint_index,
        }

    def _log_event(self, message: str) -> None:
        event = {"time": time.time(), "message": message}
        self.events.append(event)
        if len(self.events) > 500:
            self.events = self.events[-250:]
        logger.info(message)


class CaveBot:
    """Full cavebot orchestrator: captures frames and runs the state machine."""

    def __init__(self, config: BotConfig, atlas: Atlas):
        self.config = config
        self.atlas = atlas
        self.sm = BotStateMachine()
        self.capture = FrameCapture(config.capture.camera_index)
        self.locator = MinimapLocator(atlas)
        self.targeting = TargetingSystem()
        self.healer = Healer(
            health_threshold=config.healing.health_threshold,
            mana_threshold=config.healing.mana_threshold,
            health_cooldown=config.healing.health_cooldown,
            mana_cooldown=config.healing.mana_cooldown,
        )
        self.supplies = SupplyTracker(
            max_potions=config.supplies.max_potions,
            refill_threshold=config.supplies.refill_threshold,
        )
        self.spell_rotation = SpellRotation(
            spell_keys=config.combat.spell_keys,
            cooldowns=config.combat.spell_cooldowns,
        )
        self._current_path: list[tuple[int, int]] = []
        self._path_index: int = 0
        self._thread: threading.Thread | None = None
        self._stop_event = threading.Event()
        self.on_status_update: object = None

    def start(self) -> None:
        if not self.capture.open():
            logger.error("Failed to open camera")
            return
        self._stop_event.clear()
        self.sm.start()
        self._thread = threading.Thread(target=self._run_loop, daemon=True)
        self._thread.start()

    def stop(self) -> None:
        self._stop_event.set()
        self.sm.stop()
        self.capture.close()
        if self._thread is not None:
            self._thread.join(timeout=5)

    def _run_loop(self) -> None:
        while not self._stop_event.is_set():
            start_time = time.monotonic()
            try:
                self._tick()
            except Exception:
                logger.exception("Error in bot tick")
            if self.on_status_update:
                self.on_status_update(self.sm.status())
            elapsed = time.monotonic() - start_time
            sleep_time = max(0, 0.1 - elapsed)
            if sleep_time > 0:
                self._stop_event.wait(sleep_time)

    def _tick(self) -> None:
        frame = self.capture.read()
        if frame is None:
            return

        cfg = self.config

        health_bar = self.capture.crop_region(frame, cfg.capture.health_bar_region)
        mana_bar = self.capture.crop_region(frame, cfg.capture.mana_bar_region)
        self.sm.health_pct = read_bar_percentage(health_bar, channel=2)
        self.sm.mana_pct = read_bar_percentage(mana_bar, channel=0)

        heal_action = self.healer.check(self.sm.health_pct, self.sm.mana_pct)
        if heal_action == "health":
            press_key(cfg.healing.health_pot_key)
            self.healer.mark_used("health")
            self.supplies.use_potion()
        elif heal_action == "mana":
            press_key(cfg.healing.mana_pot_key)
            self.healer.mark_used("mana")
            self.supplies.use_potion()

        if self.supplies.needs_refill() and self.sm.state != BotState.REFILL:
            self.sm.transition(BotState.REFILL)
            self._plan_path_to_waypoint(cfg.supplies.depot_waypoint_index)
            return

        minimap_img = self.capture.crop_region(frame, cfg.capture.minimap_region)
        minimap_rgb = minimap_img[:, :, ::-1]
        pos = self.locator.locate(minimap_rgb, z=self.sm.position[2])
        if pos is not None:
            self.sm.position = (pos[0], pos[1], self.sm.position[2])

        state = self.sm.state

        if state == BotState.WALKING or state == BotState.REFILL:
            self._tick_walking(frame)
        elif state == BotState.COMBAT:
            self._tick_combat(frame)
        elif state == BotState.LOOTING:
            self._tick_looting(frame)

    def _tick_walking(self, frame: np.ndarray) -> None:
        cfg = self.config

        if self.sm.state != BotState.REFILL:
            battle_region = self.capture.crop_region(frame, cfg.capture.battle_list_region)
            if self.targeting.has_target(battle_region):
                self.sm.transition(BotState.COMBAT)
                attack(cfg.combat.attack_key)
                return

        if self._path_index < len(self._current_path):
            pos = self._current_path[self._path_index]
            cx, cy = self.sm.position[0], self.sm.position[1]
            dx, dy = pos[0] - cx, pos[1] - cy
            dx = max(-1, min(1, dx))
            dy = max(-1, min(1, dy))
            if dx == 0 and dy == 0:
                self._path_index += 1
            else:
                keys = []
                if dy < 0:
                    keys.append("up")
                elif dy > 0:
                    keys.append("down")
                if dx < 0:
                    keys.append("left")
                elif dx > 0:
                    keys.append("right")
                if keys:
                    press_keys_simultaneous(keys)
        else:
            self._advance_waypoint()

    def _tick_combat(self, frame: np.ndarray) -> None:
        cfg = self.config
        battle_region = self.capture.crop_region(frame, cfg.capture.battle_list_region)

        if not self.targeting.has_target(battle_region):
            self.sm.kills += 1
            self.sm.transition(BotState.LOOTING)
            return

        self.spell_rotation.cast_next()

    def _tick_looting(self, frame: np.ndarray) -> None:
        self.sm.transition(BotState.WALKING)

    def _advance_waypoint(self) -> None:
        waypoints = self.config.waypoints.list
        if not waypoints:
            return
        self.sm.waypoint_index = (self.sm.waypoint_index + 1) % len(waypoints)
        if not self.config.waypoints.loop and self.sm.waypoint_index == 0:
            self.sm.stop()
            return
        self._plan_path_to_waypoint(self.sm.waypoint_index)

    def _plan_path_to_waypoint(self, wp_index: int) -> None:
        waypoints = self.config.waypoints.list
        if wp_index >= len(waypoints):
            return
        wp = waypoints[wp_index]
        start = self.sm.position
        goal = (wp.x, wp.y, wp.z)
        path = find_path(self.atlas, start, goal)
        if path is not None:
            self._current_path = path
            self._path_index = 0
        else:
            logger.warning(f"No path found to waypoint {wp_index}")
            self._current_path = []
            self._path_index = 0
