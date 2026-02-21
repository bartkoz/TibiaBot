"""Smart loot collection with item whitelist filtering.

Flow
----
1. ``state.loot_pending`` is set by CombatModule when an enemy is defeated.
2. LootModule waits ``delay_after_kill`` seconds for the corpse to appear.
3. It shift+right-clicks each of the 8 surrounding tiles to open corpses.
4. For each opened container it scans item slots against whitelist templates.
5. Only matching items are ctrl+clicked into the backpack.
6. If whitelist = ["*"] the module takes everything (legacy mode).
7. If whitelist is empty, nothing is taken (safe default – no accidental
   backpack fill).

Loot template images
--------------------
Place one PNG per item in the ``loot/`` directory (configurable via
``loot.templates_dir`` in bot_config.yaml).  The filename without extension
must match the item name in the whitelist, e.g. ``loot/gold_coin.png``.
"""

import asyncio
import os
from random import randint
from typing import List, Optional, Tuple

import pyautogui

from bot.config import LootConfig, ViewportConfig
from bot.modules.base import BaseModule
from bot.screen import ScreenCapture
from bot.state import GameState
from bot.vision import find_template


class LootModule(BaseModule):

    def __init__(
        self,
        screen: ScreenCapture,
        state: GameState,
        config: LootConfig,
        viewport: ViewportConfig,
    ) -> None:
        super().__init__(screen, state)
        self.config = config
        self.viewport = viewport
        self._take_all: bool = "*" in config.whitelist
        self._templates: List[str] = self._resolve_templates()

    # ── setup ─────────────────────────────────────────────────────────────────

    def _resolve_templates(self) -> List[str]:
        if self._take_all:
            print("[Loot] Take-all mode – every item will be collected")
            return []
        resolved = []
        for name in self.config.whitelist:
            if name == "*":
                continue
            path = os.path.join(self.config.templates_dir, f"{name}.png")
            if os.path.exists(path):
                resolved.append(path)
                print(f"[Loot] Whitelisted: {name}")
            else:
                print(f"[Loot] WARNING: template not found: {path}")
        if not resolved and not self._take_all:
            print("[Loot] No valid whitelist templates – loot collection disabled")
        return resolved

    # ── surrounding tile positions ────────────────────────────────────────────

    def _surrounding_positions(self) -> List[Tuple[int, int]]:
        """Screen coordinates of the 8 tiles adjacent to the character."""
        cx = self.viewport.center_x
        cy = self.viewport.center_y
        ts = self.viewport.tile_size
        # Add a small random jitter to avoid pixel-perfect repeatability
        o = ts + randint(2, 8)
        return [
            (cx - o, cy),
            (cx - o, cy + o),
            (cx,     cy + o),
            (cx + o, cy + o),
            (cx + o, cy),
            (cx + o, cy - o),
            (cx,     cy - o),
            (cx - o, cy - o),
        ]

    # ── actions ──────────────────────────────────────────────────────────────

    def _open_tile(self, x: int, y: int) -> None:
        """Shift+right-click a tile to open a corpse or container on it."""
        pyautogui.keyDown("shift")
        pyautogui.click(x, y, button="right")
        pyautogui.keyUp("shift")

    def _take_item(self, x: int, y: int) -> None:
        """Ctrl+click an item slot to move it to the default container."""
        pyautogui.keyDown("ctrl")
        pyautogui.click(x, y)
        pyautogui.keyUp("ctrl")

    def _find_whitelisted_items(self, frame) -> List[Tuple[int, int]]:
        """Return (x, y) screen positions for all visible whitelisted items."""
        hits: List[Tuple[int, int]] = []
        for path in self._templates:
            result = find_template(frame, path, threshold=0.90)
            if result:
                row, col = result
                hits.append((col, row))  # return as (x, y)
        return hits

    # ── main loop ─────────────────────────────────────────────────────────────

    async def run(self) -> None:
        if not self.config.enabled:
            print("[Loot] Module disabled")
            return
        if not self._take_all and not self._templates:
            print("[Loot] Nothing to collect – module idle")
            return

        await self._wait_for_frame()

        while self.state.running:
            if not self.state.loot_pending:
                await asyncio.sleep(0.1)
                continue

            print(f"[Loot] Waiting {self.config.delay_after_kill}s for corpse…")
            self.state.looting_active = True
            await asyncio.sleep(self.config.delay_after_kill)

            positions = self._surrounding_positions()

            if self._take_all:
                # Legacy mode: blindly shift+right-click every surrounding tile
                for x, y in positions:
                    self._open_tile(x, y)
                    await asyncio.sleep(0.06)
            else:
                # Smart mode: open each tile, look for whitelisted items, take only those
                for x, y in positions:
                    self._open_tile(x, y)
                    await asyncio.sleep(0.35)  # let container window render

                    frame = self.screen.get_frame()
                    if frame is None:
                        continue

                    items = self._find_whitelisted_items(frame)
                    for item_x, item_y in items:
                        print(f"[Loot] Taking item at ({item_x},{item_y})")
                        self._take_item(item_x, item_y)
                        await asyncio.sleep(0.12)

                    # Close the container before opening the next one so
                    # windows don't stack up and obscure the game view.
                    pyautogui.press("escape")
                    await asyncio.sleep(0.1)

            self.state.loot_pending = False
            self.state.looting_active = False
            print("[Loot] Done")

            await asyncio.sleep(0.1)
