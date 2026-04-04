"""Battle list and loot window detection via pixel analysis."""

from __future__ import annotations

import numpy as np


def is_battle_list_active(
    battle_list_region: np.ndarray,
    brightness_threshold: int = 100,
    min_bright_ratio: float = 0.02,
) -> bool:
    if battle_list_region.size == 0:
        return False
    gray = np.mean(battle_list_region, axis=2)
    bright_pixels = np.sum(gray > brightness_threshold)
    total = gray.size
    return bool((bright_pixels / total) > min_bright_ratio)


def is_loot_window_open(
    screen_region: np.ndarray,
    brightness_threshold: int = 100,
    min_bright_ratio: float = 0.15,
) -> bool:
    if screen_region.size == 0:
        return False
    gray = np.mean(screen_region, axis=2)
    bright_pixels = np.sum(gray > brightness_threshold)
    total = gray.size
    return bool((bright_pixels / total) > min_bright_ratio)
