"""Enemy detection, attack management, and reachability tracking.

Reachability logic
------------------
After initiating an attack, the module watches how long the character has
been stationary.  If the position hasn't changed for ``stuck_timeout``
seconds, the current target area is flagged as unreachable and the attack
is cancelled so navigation can move on.
"""

import asyncio
import time
from typing import Optional

import pyautogui

from bot.config import CombatConfig
from bot.modules.base import BaseModule
from bot.screen import ScreenCapture
from bot.state import GameState, Position
from bot.vision import find_template, pixel_rgb

_NO_ENEMY_RGB = (70, 70, 70)  # colour of an empty battle-list slot


class CombatModule(BaseModule):

    def __init__(
        self, screen: ScreenCapture, state: GameState, config: CombatConfig
    ) -> None:
        super().__init__(screen, state)
        self.config = config

        # Pixel anchor for the battle-list indicator – located once at startup
        self._battle_pixel: Optional[tuple] = None   # (row, col) to sample
        self._follow_pos: Optional[tuple] = None      # (row, col) of Follow button

        self._attack_started_at: Optional[float] = None
        self._pos_at_attack_start: Optional[Position] = None

    # ── startup ──────────────────────────────────────────────────────────────

    async def _setup(self) -> None:
        frame = self.screen.get_frame()
        while frame is None:
            await asyncio.sleep(0.1)
            frame = self.screen.get_frame()

        bp = find_template(frame, "images/battle.png")
        if bp:
            # Offset into the first battle-slot cell (empirical, same as original)
            self._battle_pixel = (bp[0] + 20, bp[1] + 6)
            print(f"[Combat] Battle indicator at row={self._battle_pixel[0]} col={self._battle_pixel[1]}")
        else:
            print("[Combat] WARNING: images/battle.png not found – enemy detection disabled")

        fp = find_template(frame, "images/follow.png")
        if fp:
            self._follow_pos = fp
            print(f"[Combat] Follow button at row={fp[0]} col={fp[1]}")
        else:
            print("[Combat] WARNING: images/follow.png not found")

    # ── detection helpers ────────────────────────────────────────────────────

    def _enemy_present(self, frame) -> bool:
        """Single-pixel check on the battle list – O(1), very fast."""
        if self._battle_pixel is None:
            return False
        r, c = self._battle_pixel
        rgb = pixel_rgb(frame, x=c, y=r)
        return rgb != _NO_ENEMY_RGB

    def _is_attacking(self, frame) -> bool:
        """Template-match check for the active-attack sprite overlay."""
        for name in self.config.monsters:
            if find_template(frame, f"images/{name}_attacking.png", threshold=0.98):
                return True
        return False

    # ── actions ──────────────────────────────────────────────────────────────

    def _start_attack(self) -> None:
        if self._follow_pos:
            row, col = self._follow_pos
            pyautogui.click(col, row)
        pyautogui.press(self.config.attack_key)
        self._attack_started_at = time.monotonic()
        self._pos_at_attack_start = self.state.position
        self.state.loot_pending = False

    def _cancel_attack(self) -> None:
        pyautogui.press("escape")
        self._attack_started_at = None
        self._pos_at_attack_start = None
        self.state.currently_attacking = False

    # ── reachability ─────────────────────────────────────────────────────────

    def _stuck_seconds(self) -> float:
        """Return how long we've been attacking without moving, or 0."""
        if self._attack_started_at is None:
            return 0.0
        if self.state.seconds_since_last_move() < 0.5:
            # We moved recently – reset the attack timer
            self._attack_started_at = time.monotonic()
            return 0.0
        return time.monotonic() - self._attack_started_at

    # ── main loop ────────────────────────────────────────────────────────────

    async def run(self) -> None:
        await self._wait_for_frame()
        await self._setup()

        while self.state.running:
            frame = self.screen.get_frame()
            if frame is None:
                await asyncio.sleep(0.05)
                continue

            enemy = self._enemy_present(frame)
            self.state.enemy_in_battle_list = enemy

            if enemy:
                attacking = self._is_attacking(frame)
                self.state.currently_attacking = attacking

                if not attacking:
                    # New target available – start attacking
                    print("[Combat] Enemy detected – attacking")
                    self._start_attack()

                else:
                    # Already attacking – watch for stuck condition
                    stuck = self._stuck_seconds()
                    if stuck > self.config.stuck_timeout:
                        pos = self.state.position
                        print(
                            f"[Combat] Stuck {stuck:.1f}s at ({pos.x},{pos.y}) – "
                            f"marking unreachable for {self.config.unreachable_cooldown}s"
                        )
                        self.state.mark_unreachable(pos, self.config.unreachable_cooldown)
                        self._cancel_attack()
                        # Signal loot module to try picking up whatever dropped
                        self.state.loot_pending = True

            else:
                # No enemy in battle list
                if self.state.currently_attacking:
                    print("[Combat] Enemy defeated – switching to loot")
                    pyautogui.keyUp(self.config.attack_key)
                    self.state.currently_attacking = False
                    self._attack_started_at = None
                    self.state.loot_pending = True

            await asyncio.sleep(0.05)
