import numpy as np
from cavebot.vision.detection import is_battle_list_active, is_loot_window_open


def test_battle_list_empty():
    region = np.zeros((300, 180, 3), dtype=np.uint8)
    region[:] = 20
    assert is_battle_list_active(region) is False


def test_battle_list_has_entries():
    region = np.zeros((300, 180, 3), dtype=np.uint8)
    region[:] = 20
    region[10:25, 20:160] = 200
    assert is_battle_list_active(region) is True


def test_loot_window_not_open():
    region = np.zeros((200, 200, 3), dtype=np.uint8)
    assert is_loot_window_open(region) is False


def test_loot_window_open():
    region = np.zeros((200, 200, 3), dtype=np.uint8)
    region[:] = 20
    region[20:180, 20:180] = 180
    assert is_loot_window_open(region) is True
