import numpy as np
from cavebot.vision.health import read_bar_percentage


def test_full_health_bar():
    bar = np.zeros((15, 200, 3), dtype=np.uint8)
    bar[:, :, 2] = 200
    pct = read_bar_percentage(bar, channel=2, threshold=100)
    assert pct >= 95


def test_half_health_bar():
    bar = np.zeros((15, 200, 3), dtype=np.uint8)
    bar[:, :100, 2] = 200
    pct = read_bar_percentage(bar, channel=2, threshold=100)
    assert 45 <= pct <= 55


def test_empty_health_bar():
    bar = np.zeros((15, 200, 3), dtype=np.uint8)
    pct = read_bar_percentage(bar, channel=2, threshold=100)
    assert pct <= 5


def test_full_mana_bar():
    bar = np.zeros((15, 200, 3), dtype=np.uint8)
    bar[:, :, 0] = 200
    pct = read_bar_percentage(bar, channel=0, threshold=100)
    assert pct >= 95


def test_quarter_mana_bar():
    bar = np.zeros((15, 200, 3), dtype=np.uint8)
    bar[:, :50, 0] = 200
    pct = read_bar_percentage(bar, channel=0, threshold=100)
    assert 20 <= pct <= 30
