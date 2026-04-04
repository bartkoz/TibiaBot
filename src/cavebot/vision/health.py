"""Health and mana bar pixel reading."""

from __future__ import annotations

import numpy as np


def read_bar_percentage(
    bar_image: np.ndarray,
    channel: int,
    threshold: int = 100,
) -> float:
    if bar_image.size == 0:
        return 0.0
    mid_row = bar_image.shape[0] // 2
    row = bar_image[mid_row, :, channel]
    filled = int(np.sum(row >= threshold))
    total = len(row)
    if total == 0:
        return 0.0
    return (filled / total) * 100.0
