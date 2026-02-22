"""Minimap visual-odometry navigation module.

Navigates by template-matching minimap snapshots recorded at each waypoint
against the live minimap feed.  No coordinate display or OCR is required.

How it works
------------
1. At record time (record_minimap_waypoints.py), the user stands at each
   waypoint and presses INSERT.  A ``template_size × template_size`` greyscale
   crop of the minimap is saved; the centre pixels (player dot) are blanked so
   they don't interfere with matching.

2. At run time the module continuously grabs the minimap, matches the *next*
   waypoint's template inside the full minimap image, and reads the match
   offset from minimap centre.  That offset tells us which direction to click
   in the game viewport.

3. Arrival: when the template matches within ``arrival_px`` pixels of minimap
   centre the waypoint is considered reached and the index advances.

4. Stuck detection: if the minimap image does not change over ``stuck_timeout``
   seconds the module skips the current target waypoint and tries the next one.

Limitations
-----------
All waypoints must be within minimap view of each other simultaneously (i.e.
their separation must be < (minimap_width - template_size) / 2 pixels).  For
the default 86 × 40 configuration that is 23 minimap pixels ≈ 23 game tiles,
which is enough for a typical small hunting area.
"""

import asyncio
import json
import math
import os
import time
from typing import List, Optional, Tuple

import cv2
import numpy as np
import pyautogui

from bot.config import MinimapConfig, ViewportConfig
from bot.modules.base import BaseModule
from bot.screen import ScreenCapture
from bot.state import GameState


# Confidence below which a template match is considered "not visible"
_MIN_CONFIDENCE = 0.65
# Radius of player-dot mask blanked from every template (pixels)
_DOT_RADIUS = 4


class MinimapNavigationModule(BaseModule):

    def __init__(
        self,
        screen: ScreenCapture,
        state: GameState,
        config: MinimapConfig,
        viewport: ViewportConfig,
    ) -> None:
        super().__init__(screen, state)
        self.config   = config
        self.viewport = viewport
        self._templates: List[np.ndarray] = []
        self._wp_idx: int = 0
        self._last_minimap: Optional[np.ndarray] = None
        self._stuck_since: float = 0.0
        self._last_move_at: float = 0.0

    # ── template loading ──────────────────────────────────────────────────────

    def _load_templates(self) -> bool:
        path = self.config.waypoints_file
        if not path:
            print("[MinimapNav] No waypoints_file configured")
            return False
        if not os.path.exists(path):
            print(f"[MinimapNav] waypoints_file not found: {path}")
            return False

        with open(path) as f:
            data = json.load(f)

        for img_path in data.get("waypoints", []):
            tmpl = cv2.imread(img_path, cv2.IMREAD_GRAYSCALE)
            if tmpl is None:
                print(f"[MinimapNav] WARNING: could not load template {img_path}")
                continue
            self._templates.append(tmpl)

        if not self._templates:
            print("[MinimapNav] No valid templates loaded")
            return False

        print(f"[MinimapNav] Loaded {len(self._templates)} waypoint templates")
        return True

    # ── minimap capture ───────────────────────────────────────────────────────

    def _get_minimap(self, frame: np.ndarray) -> Optional[np.ndarray]:
        c = self.config
        roi = frame[c.y : c.y + c.height, c.x : c.x + c.width]
        if roi.shape[0] < 10 or roi.shape[1] < 10:
            return None
        gray = cv2.cvtColor(roi[:, :, :3], cv2.COLOR_BGR2GRAY)
        return gray

    # ── template matching ─────────────────────────────────────────────────────

    def _find_template(
        self, minimap: np.ndarray, template: np.ndarray
    ) -> Optional[Tuple[int, int, float]]:
        """Match *template* in *minimap*.

        Returns ``(dx, dy, confidence)`` where ``(dx, dy)`` is the pixel
        offset from minimap centre to the matched template centre, or ``None``
        if the match confidence is below threshold.
        """
        th, tw = template.shape[:2]
        mh, mw = minimap.shape[:2]
        if mh < th or mw < tw:
            return None

        result = cv2.matchTemplate(minimap, template, cv2.TM_CCORR_NORMED)
        _, max_val, _, max_loc = cv2.minMaxLoc(result)

        if max_val < _MIN_CONFIDENCE:
            return None

        # Centre of the matched region
        match_cx = max_loc[0] + tw // 2
        match_cy = max_loc[1] + th // 2

        # Player is at minimap centre
        player_cx = mw // 2
        player_cy = mh // 2

        return match_cx - player_cx, match_cy - player_cy, max_val

    # ── movement ──────────────────────────────────────────────────────────────

    def _click_toward(self, dx: int, dy: int) -> None:
        """Click the game viewport 3 tiles in the direction (dx, dy)."""
        dist = math.hypot(dx, dy)
        if dist < 1:
            return
        nx, ny = dx / dist, dy / dist
        tiles = 3
        cx = self.viewport.center_x + int(nx * self.viewport.tile_size * tiles)
        cy = self.viewport.center_y + int(ny * self.viewport.tile_size * tiles)
        # Clamp to viewport
        cx = max(self.viewport.left + self.viewport.tile_size,
                 min(self.viewport.left + self.viewport.width  - self.viewport.tile_size, cx))
        cy = max(self.viewport.top  + self.viewport.tile_size,
                 min(self.viewport.top  + self.viewport.height - self.viewport.tile_size, cy))
        pyautogui.click(cx, cy)

    # ── stuck detection ───────────────────────────────────────────────────────

    def _minimap_moved(self, current: np.ndarray) -> bool:
        """Return True if the minimap has changed since last check."""
        if self._last_minimap is None:
            return True
        diff = cv2.absdiff(current, self._last_minimap)
        return float(diff.mean()) > 0.8

    # ── main loop ─────────────────────────────────────────────────────────────

    async def run(self) -> None:
        if not self._load_templates():
            print("[MinimapNav] Module idle – no valid waypoint templates")
            return

        await self._wait_for_frame()
        self._stuck_since = time.monotonic()
        print(f"[MinimapNav] Started – {len(self._templates)} waypoints, "
              f"arrival threshold={self.config.arrival_px}px")

        while self.state.running:

            # Let combat and looting take priority
            if self.state.enemy_in_battle_list or self.state.looting_active:
                await asyncio.sleep(0.15)
                continue

            frame = self.screen.get_frame()
            if frame is None:
                await asyncio.sleep(0.1)
                continue

            minimap = self._get_minimap(frame)
            if minimap is None:
                await asyncio.sleep(0.1)
                continue

            # ── which waypoint are we heading toward? ────────────────────────
            target_idx = (self._wp_idx + 1) % len(self._templates)
            template   = self._templates[target_idx]
            result     = self._find_template(minimap, template)

            if result is None:
                # Target not visible in current minimap; wait and retry
                await asyncio.sleep(self.config.move_interval)
                continue

            dx, dy, conf = result
            dist = math.hypot(dx, dy)

            # ── arrival check ────────────────────────────────────────────────
            if dist <= self.config.arrival_px:
                print(f"[MinimapNav] Reached waypoint {target_idx} "
                      f"(conf={conf:.2f}, dist={dist:.1f}px)")
                self._wp_idx      = target_idx
                self._stuck_since = time.monotonic()
                self._last_minimap = None
                await asyncio.sleep(0.3)
                continue

            # ── stuck detection ──────────────────────────────────────────────
            now = time.monotonic()
            if self._minimap_moved(minimap):
                self._stuck_since = now
            elif now - self._stuck_since > self.config.stuck_timeout:
                print(f"[MinimapNav] Stuck for {self.config.stuck_timeout}s "
                      f"– skipping to waypoint {target_idx}")
                self._wp_idx      = target_idx
                self._stuck_since = now
                self._last_minimap = None
                await asyncio.sleep(0.3)
                continue

            self._last_minimap = minimap.copy()

            # ── navigate ─────────────────────────────────────────────────────
            if now - self._last_move_at >= self.config.move_interval:
                if dist > self.config.arrival_px + 2:
                    print(f"[MinimapNav] WP {target_idx} "
                          f"Δ=({dx:+d},{dy:+d}) dist={dist:.0f}px conf={conf:.2f}")
                self._click_toward(dx, dy)
                self._last_move_at = now

            await asyncio.sleep(0.1)
