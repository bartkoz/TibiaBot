"""HP monitoring and automatic healing."""

import asyncio
import time
from typing import Optional

import pyautogui

from bot.config import HealingConfig
from bot.modules.base import BaseModule
from bot.screen import ScreenCapture
from bot.state import GameState
from bot.vision import find_template, read_bar_percent

_HP_COLOR_RGB = (255, 113, 113)
_TEMPLATE = "images/health.png"
_OFFSET_X = 5   # relative to template match position
_OFFSET_Y = 7
_BAR_WIDTH = 92  # pixel width of the HP bar at 100 %


class HealthModule(BaseModule):
    """Reads the HP bar each tick and presses *heal_key* when HP is low."""

    def __init__(
        self, screen: ScreenCapture, state: GameState, config: HealingConfig
    ) -> None:
        super().__init__(screen, state)
        self.config = config
        self._bar_x: Optional[int] = None
        self._bar_y: Optional[int] = None
        self._last_heal_at: float = 0.0
        self._heal_cooldown: float = 0.8  # minimum seconds between heals

    def _locate_bar(self, frame) -> bool:
        pos = find_template(frame, _TEMPLATE)
        if pos is None:
            return False
        self._bar_y = pos[0] + _OFFSET_Y
        self._bar_x = pos[1] + _OFFSET_X
        return True

    async def run(self) -> None:
        await self._wait_for_frame()

        # Locate bar once at startup
        while self._bar_x is None:
            frame = self.screen.get_frame()
            if frame is not None and self._locate_bar(frame):
                print(f"[Health] Bar located at x={self._bar_x} y={self._bar_y}")
            else:
                print("[Health] Waiting for HP bar...")
            await asyncio.sleep(1.0)

        while self.state.running:
            frame = self.screen.get_frame()
            if frame is None:
                await asyncio.sleep(0.05)
                continue

            hp = read_bar_percent(frame, self._bar_x, self._bar_y, _BAR_WIDTH, _HP_COLOR_RGB)
            self.state.hp_percent = hp

            now = time.monotonic()
            if (
                hp < self.config.hp_threshold
                and (now - self._last_heal_at) >= self._heal_cooldown
            ):
                print(f"[Health] HP {hp:.0f}% < {self.config.hp_threshold}% → {self.config.heal_key}")
                pyautogui.press(self.config.heal_key)
                self._last_heal_at = now

            await asyncio.sleep(0.05)  # 20 Hz
