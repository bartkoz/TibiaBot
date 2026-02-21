"""Coordinate-based waypoint navigation.

Instead of template-matching minimap screenshots (which require a new
screenshot per location and break on zoom/resolution changes), this module:

1. Reads the current world position (X, Y, Z) from the on-screen coordinate
   display via OCR.
2. Compares to the next target waypoint (stored as a plain coordinate tuple
   in bot_config.yaml).
3. Navigates by clicking directly in the game viewport at the screen position
   that corresponds to the target tile.

The game's built-in pathfinder handles obstacles for short distances.
For longer distances the module clicks repeatedly, moving closer each time.

Yielding to other modules
-------------------------
Navigation checks ``state.enemy_in_battle_list`` and ``state.looting_active``
at the top of every loop iteration.  If either is True the module sleeps
briefly and retries – combat and looting always take priority.
"""

import asyncio
import time
from typing import List, Optional, Tuple

import pyautogui

from bot.config import CoordDisplayConfig, NavigationConfig, ViewportConfig
from bot.modules.base import BaseModule
from bot.screen import ScreenCapture
from bot.state import GameState, Position
from bot.vision import ocr_available, read_coordinates_ocr


class NavigationModule(BaseModule):

    def __init__(
        self,
        screen: ScreenCapture,
        state: GameState,
        nav_cfg: NavigationConfig,
        viewport: ViewportConfig,
        coord_cfg: CoordDisplayConfig,
    ) -> None:
        super().__init__(screen, state)
        self.waypoints: List[Tuple[int, int, int]] = nav_cfg.waypoints
        self.tolerance: int = nav_cfg.waypoint_tolerance
        self.move_interval: float = nav_cfg.move_interval
        self.viewport = viewport
        self.coord_cfg = coord_cfg
        self._last_move_at: float = 0.0
        self._ocr_failures: int = 0

    # ── position reading ─────────────────────────────────────────────────────

    def _read_position(self) -> Optional[Position]:
        """OCR the minimap coordinate area and return a Position or None."""
        roi = self.screen.get_roi(
            self.coord_cfg.x,
            self.coord_cfg.y,
            self.coord_cfg.width,
            self.coord_cfg.height,
        )
        if roi is None:
            return None
        result = read_coordinates_ocr(roi)
        if result:
            self._ocr_failures = 0
            return Position(x=result[0], y=result[1], z=result[2])
        self._ocr_failures += 1
        return None

    # ── movement ─────────────────────────────────────────────────────────────

    def _click_toward(self, current: Position, target: Tuple[int, int, int]) -> None:
        """Click the game viewport at the tile closest to *target*.

        The viewport is centred on the character.  One tile = tile_size pixels.
        Movement is clamped to the visible area so we always click a reachable
        tile; the game pathfinder bridges the remaining distance on the next
        click.
        """
        vp = self.viewport
        dx = target[0] - current.x
        dy = target[1] - current.y

        # How many tiles fit from centre to edge (leave 1 tile margin)
        half_x = (vp.width  // vp.tile_size) // 2 - 1
        half_y = (vp.height // vp.tile_size) // 2 - 1

        dx = max(-half_x, min(half_x, dx))
        dy = max(-half_y, min(half_y, dy))

        click_x = vp.center_x + dx * vp.tile_size
        click_y = vp.center_y + dy * vp.tile_size

        # Safety clamp to viewport bounds
        click_x = max(vp.left + vp.tile_size, min(vp.left + vp.width  - vp.tile_size, click_x))
        click_y = max(vp.top  + vp.tile_size, min(vp.top  + vp.height - vp.tile_size, click_y))

        pyautogui.click(click_x, click_y)

    # ── main loop ────────────────────────────────────────────────────────────

    async def run(self) -> None:
        if not self.waypoints:
            print("[Navigation] No waypoints configured – module idle")
            return

        if not ocr_available():
            print(
                "[Navigation] pytesseract not installed – coordinate reading disabled.\n"
                "  Install with: pip install pytesseract  (and Tesseract-OCR)"
            )
            return

        await self._wait_for_frame()
        print(f"[Navigation] {len(self.waypoints)} waypoints loaded")

        while self.state.running:
            # ── yield to higher-priority modules ────────────────────────────
            if self.state.enemy_in_battle_list or self.state.looting_active:
                await asyncio.sleep(0.15)
                continue

            # ── read current position ────────────────────────────────────────
            pos = self._read_position()
            if pos is not None:
                self.state.update_position(pos)
            else:
                if self._ocr_failures % 20 == 1:
                    print(
                        f"[Navigation] OCR failed {self._ocr_failures}× – "
                        "check coord_display region in bot_config.yaml"
                    )
                if self.state.position_stale(max_age=5.0):
                    await asyncio.sleep(0.2)
                    continue
                pos = self.state.position  # use last known

            # ── check if target waypoint reached ────────────────────────────
            target = self.waypoints[self.state.waypoint_index]
            dx = abs(pos.x - target[0])
            dy = abs(pos.y - target[1])

            if dx <= self.tolerance and dy <= self.tolerance and pos.z == target[2]:
                prev = self.state.waypoint_index
                self.state.waypoint_index = (prev + 1) % len(self.waypoints)
                print(
                    f"[Navigation] Waypoint {prev + 1}/{len(self.waypoints)} reached "
                    f"→ moving to {self.state.waypoint_index + 1}"
                )
                await asyncio.sleep(0.3)
                continue

            # ── navigate toward target ───────────────────────────────────────
            now = time.monotonic()
            if now - self._last_move_at >= self.move_interval:
                if dx > 2 or dy > 2:
                    print(
                        f"[Navigation] WP {self.state.waypoint_index + 1} "
                        f"({target[0]},{target[1]}) "
                        f"curr=({pos.x},{pos.y}) Δ=({dx},{dy})"
                    )
                self._click_toward(pos, target)
                self._last_move_at = now

            await asyncio.sleep(0.1)
