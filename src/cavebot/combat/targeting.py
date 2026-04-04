"""Monster targeting from battle list region."""

from __future__ import annotations

import numpy as np

from cavebot.vision.detection import is_battle_list_active


class TargetingSystem:
    def has_target(self, battle_list_region: np.ndarray) -> bool:
        return is_battle_list_active(battle_list_region)
