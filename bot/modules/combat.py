"""Enemy detection, attack management, and reachability tracking.

Attack detection without per-monster screenshots
-------------------------------------------------
Tibia draws a pure-red (R=255 G=0 B=0) 1-px border around the battle-list
entry of the currently selected monster.  Checking a small pixel region at
the corner of that entry is all we need to know whether we are attacking,
regardless of monster type.

``attack_indicator_offset`` in config controls where that corner is relative
to the enemy-detection pixel (_battle_pixel).  The default (-10, -10) is
derived from the standard client where each battle-list entry is 20×20 px:
the enemy-detection pixel sits at the centre (+10, +10 from entry top-left),
so the red corner is 10 rows above and 10 columns to the left.

Reachability logic
------------------
If the character's position has not changed for ``stuck_timeout`` seconds
while an attack is in progress, the area is flagged as temporarily
unreachable and the attack is cancelled so navigation can resume.
"""

import asyncio
import os
import time
from typing import Optional, Tuple

import numpy as np
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

        # Located once at startup via template matching; all subsequent
        # checks are pure pixel comparisons — no per-monster images needed.
        self._battle_pixel: Optional[Tuple[int, int]] = None   # (row, col)
        self._attack_indicator: Optional[Tuple[int, int]] = None  # (row, col)
        self._follow_pos: Optional[Tuple[int, int]] = None

        self._attack_started_at: Optional[float] = None
        self._pos_at_attack_start: Optional[Position] = None

    # ── startup ──────────────────────────────────────────────────────────────

    async def _setup(self) -> None:
        """Locate UI anchors by template matching; retries until found."""
        battle_path = "images/battle.png"
        follow_path = "images/follow.png"

        for path in (battle_path, follow_path):
            if not os.path.exists(path):
                print(f"[Combat] ERROR: {path} is missing")

        while self._battle_pixel is None or self._follow_pos is None:
            frame = self.screen.get_frame()
            if frame is not None:
                if self._battle_pixel is None:
                    bp = find_template(frame, battle_path)
                    if bp:
                        self._battle_pixel = (bp[0] + 20, bp[1] + 6)
                        r_off, c_off = self.config.attack_indicator_offset
                        self._attack_indicator = (
                            self._battle_pixel[0] + r_off,
                            self._battle_pixel[1] + c_off,
                        )
                        print(
                            f"[Combat] Battle list found – "
                            f"enemy pixel=({self._battle_pixel[1]},{self._battle_pixel[0]})  "
                            f"attack indicator=({self._attack_indicator[1]},{self._attack_indicator[0]})"
                        )

                if self._follow_pos is None:
                    fp = find_template(frame, follow_path)
                    if fp:
                        self._follow_pos = fp
                        print(f"[Combat] Follow button at row={fp[0]} col={fp[1]}")

            if self._battle_pixel is None or self._follow_pos is None:
                await asyncio.sleep(1.0)

    # ── detection ────────────────────────────────────────────────────────────

    def _enemy_present(self, frame) -> bool:
        """Single-pixel check: is there any enemy in the battle list?"""
        if self._battle_pixel is None:
            return False
        r, c = self._battle_pixel
        return pixel_rgb(frame, x=c, y=r) != _NO_ENEMY_RGB

    def _is_attacking(self, frame) -> bool:
        """Check for the red attack-indicator border on the selected entry.

        Samples a 3×3 pixel region at the top-left corner of the first
        battle-list slot.  If ≥ 3 pixels are pure red (R>200, G<50, B<50)
        the character is actively attacking.

        No per-monster screenshots required — works for any enemy type.
        """
        if self._attack_indicator is None:
            return False
        row, col = self._attack_indicator
        region = frame[row : row + 3, col : col + 3, :3]
        if region.size == 0:
            return False
        red = np.sum(
            (region[:, :, 2] > 200) &   # R (index 2 in BGR)
            (region[:, :, 1] < 50) &    # G
            (region[:, :, 0] < 50)      # B
        )
        return int(red) >= 3

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
        if self._attack_started_at is None:
            return 0.0
        if self.state.seconds_since_last_move() < 0.5:
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
                    print("[Combat] Enemy detected – attacking")
                    self._start_attack()
                else:
                    stuck = self._stuck_seconds()
                    if stuck > self.config.stuck_timeout:
                        pos = self.state.position
                        print(
                            f"[Combat] Stuck {stuck:.1f}s at ({pos.x},{pos.y}) – "
                            f"marking unreachable for {self.config.unreachable_cooldown}s"
                        )
                        self.state.mark_unreachable(pos, self.config.unreachable_cooldown)
                        self._cancel_attack()
                        self.state.loot_pending = True
            else:
                if self.state.currently_attacking:
                    print("[Combat] Enemy defeated – switching to loot")
                    pyautogui.keyUp(self.config.attack_key)
                    self.state.currently_attacking = False
                    self._attack_started_at = None
                    self.state.loot_pending = True

            await asyncio.sleep(0.05)
