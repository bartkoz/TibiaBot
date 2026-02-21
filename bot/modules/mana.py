"""Mana monitoring and automatic recovery."""

import asyncio
import time
from typing import Optional

import pyautogui

from bot.config import HealingConfig
from bot.modules.base import BaseModule
from bot.screen import ScreenCapture
from bot.state import GameState
from bot.vision import find_template, read_bar_percent

_MANA_COLOR_RGB = (101, 98, 240)
_TEMPLATE = "images/mana.png"
_OFFSET_X = 5
_OFFSET_Y = 6
_BAR_WIDTH = 92


class ManaModule(BaseModule):
    """Reads the mana bar each tick and presses *mana_key* when mana is low."""

    def __init__(
        self, screen: ScreenCapture, state: GameState, config: HealingConfig
    ) -> None:
        super().__init__(screen, state)
        self.config = config
        self._bar_x: Optional[int] = None
        self._bar_y: Optional[int] = None
        self._last_use_at: float = 0.0
        self._cooldown: float = 1.0

    def _locate_bar(self, frame) -> bool:
        pos = find_template(frame, _TEMPLATE)
        if pos is None:
            return False
        self._bar_y = pos[0] + _OFFSET_Y
        self._bar_x = pos[1] + _OFFSET_X
        return True

    async def run(self) -> None:
        if not self.config.mana_key:
            print("[Mana] No mana_key configured – module disabled")
            return

        await self._wait_for_frame()

        while self._bar_x is None:
            frame = self.screen.get_frame()
            if frame is not None and self._locate_bar(frame):
                print(f"[Mana] Bar located at x={self._bar_x} y={self._bar_y}")
            else:
                print("[Mana] Waiting for mana bar...")
            await asyncio.sleep(1.0)

        while self.state.running:
            frame = self.screen.get_frame()
            if frame is None:
                await asyncio.sleep(0.05)
                continue

            mana = read_bar_percent(frame, self._bar_x, self._bar_y, _BAR_WIDTH, _MANA_COLOR_RGB)
            self.state.mana_percent = mana

            now = time.monotonic()
            if (
                mana < self.config.mana_threshold
                and (now - self._last_use_at) >= self._cooldown
            ):
                print(f"[Mana] {mana:.0f}% < {self.config.mana_threshold}% → {self.config.mana_key}")
                pyautogui.press(self.config.mana_key)
                self._last_use_at = now

            await asyncio.sleep(0.05)
