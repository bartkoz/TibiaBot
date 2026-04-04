import numpy as np
from cavebot.combat.targeting import TargetingSystem


def test_no_target_when_battle_list_empty():
    ts = TargetingSystem()
    region = np.zeros((300, 180, 3), dtype=np.uint8)
    assert ts.has_target(region) is False


def test_has_target_when_battle_list_active():
    ts = TargetingSystem()
    region = np.zeros((300, 180, 3), dtype=np.uint8)
    region[10:25, 20:160] = 200
    assert ts.has_target(region) is True
