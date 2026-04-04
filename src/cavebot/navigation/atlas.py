"""Minimap atlas — loads PNG tiles into an indexed structure."""

from __future__ import annotations

import re
from pathlib import Path

import numpy as np
from PIL import Image

_COLOR_RE = re.compile(r"Minimap_Color_(\d+)_(\d+)_(\d+)\.png")
_COST_RE = re.compile(r"Minimap_WaypointCost_(\d+)_(\d+)_(\d+)\.png")
CHUNK_SIZE = 256


class Atlas:
    def __init__(self, minimap_dir: str | Path):
        self.minimap_dir = Path(minimap_dir)
        self.color_chunks: dict[tuple[int, int, int], np.ndarray] = {}
        self.cost_chunks: dict[tuple[int, int, int], np.ndarray] = {}
        self.z_levels: set[int] = set()

    def load(self) -> None:
        """Scan minimap directory and load all tiles into memory."""
        for fpath in self.minimap_dir.iterdir():
            m = _COLOR_RE.match(fpath.name)
            if m:
                cx, cy, cz = int(m.group(1)), int(m.group(2)), int(m.group(3))
                img = Image.open(fpath).convert("RGB")
                self.color_chunks[(cx, cy, cz)] = np.array(img)
                self.z_levels.add(cz)
                continue
            m = _COST_RE.match(fpath.name)
            if m:
                cx, cy, cz = int(m.group(1)), int(m.group(2)), int(m.group(3))
                img = Image.open(fpath)
                self.cost_chunks[(cx, cy, cz)] = np.array(img)
                self.z_levels.add(cz)

    def get_color_tile(self, chunk_x: int, chunk_y: int, z: int) -> np.ndarray | None:
        return self.color_chunks.get((chunk_x, chunk_y, z))

    def get_cost_tile(self, chunk_x: int, chunk_y: int, z: int) -> np.ndarray | None:
        return self.cost_chunks.get((chunk_x, chunk_y, z))

    def get_cost_at(self, world_x: int, world_y: int, z: int) -> int:
        """Get walk cost at an absolute world coordinate. Returns 255 if missing."""
        chunk_x = (world_x // CHUNK_SIZE) * CHUNK_SIZE
        chunk_y = (world_y // CHUNK_SIZE) * CHUNK_SIZE
        tile = self.cost_chunks.get((chunk_x, chunk_y, z))
        if tile is None:
            return 255
        px = world_x - chunk_x
        py = world_y - chunk_y
        if 0 <= px < CHUNK_SIZE and 0 <= py < CHUNK_SIZE:
            return int(tile[py, px])
        return 255

    def chunk_keys_for_z(self, z: int) -> list[tuple[int, int, int]]:
        return [k for k in self.color_chunks if k[2] == z]

    def world_bounds(self, z: int) -> dict[str, int]:
        keys = self.chunk_keys_for_z(z)
        if not keys:
            return {"min_x": 0, "min_y": 0, "max_x": 0, "max_y": 0}
        min_x = min(k[0] for k in keys)
        min_y = min(k[1] for k in keys)
        max_x = max(k[0] for k in keys) + CHUNK_SIZE
        max_y = max(k[1] for k in keys) + CHUNK_SIZE
        return {"min_x": min_x, "min_y": min_y, "max_x": max_x, "max_y": max_y}
