"""Minimap position detection via template matching."""

from __future__ import annotations

import cv2
import numpy as np

from cavebot.navigation.atlas import CHUNK_SIZE, Atlas


class MinimapLocator:
    def __init__(self, atlas: Atlas):
        self._atlas = atlas
        self._last_chunk: tuple[int, int, int] | None = None
        self._last_pos: tuple[int, int] | None = None

    def locate(
        self,
        minimap_snippet: np.ndarray,
        z: int,
        confidence_threshold: float = 0.7,
    ) -> tuple[int, int, float] | None:
        snippet_h, snippet_w = minimap_snippet.shape[:2]

        if np.std(minimap_snippet) == 0:
            return None

        if self._last_chunk is not None and self._last_chunk[2] == z:
            chunks = self._get_neighbor_chunks(self._last_chunk)
        else:
            chunks = self._atlas.chunk_keys_for_z(z)

        best_score = -1.0
        best_pos: tuple[int, int] | None = None
        best_chunk: tuple[int, int, int] | None = None

        for chunk_key in chunks:
            tile = self._atlas.get_color_tile(*chunk_key)
            if tile is None:
                continue
            if tile.shape[0] < snippet_h or tile.shape[1] < snippet_w:
                continue

            result = cv2.matchTemplate(tile, minimap_snippet, cv2.TM_CCOEFF_NORMED)
            _, max_val, _, max_loc = cv2.minMaxLoc(result)

            if max_val > best_score:
                best_score = max_val
                best_pos = (chunk_key[0] + max_loc[0], chunk_key[1] + max_loc[1])
                best_chunk = chunk_key

        if best_score < confidence_threshold or best_pos is None:
            if self._last_chunk is not None:
                self._last_chunk = None
                return self.locate(minimap_snippet, z, confidence_threshold)
            return None

        self._last_chunk = best_chunk
        self._last_pos = best_pos
        return (best_pos[0], best_pos[1], best_score)

    def _get_neighbor_chunks(
        self, center: tuple[int, int, int]
    ) -> list[tuple[int, int, int]]:
        cx, cy, cz = center
        neighbors = []
        for dx in [-CHUNK_SIZE, 0, CHUNK_SIZE]:
            for dy in [-CHUNK_SIZE, 0, CHUNK_SIZE]:
                key = (cx + dx, cy + dy, cz)
                if key in self._atlas.color_chunks:
                    neighbors.append(key)
        return neighbors
