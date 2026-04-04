import time
from cavebot.survival.healer import Healer


def test_healer_needs_health_pot():
    h = Healer(health_threshold=60, mana_threshold=40, health_cooldown=1.0, mana_cooldown=1.0)
    action = h.check(health_pct=30.0, mana_pct=80.0)
    assert action == "health"


def test_healer_needs_mana_pot():
    h = Healer(health_threshold=60, mana_threshold=40, health_cooldown=1.0, mana_cooldown=1.0)
    action = h.check(health_pct=90.0, mana_pct=20.0)
    assert action == "mana"


def test_healer_no_action_needed():
    h = Healer(health_threshold=60, mana_threshold=40, health_cooldown=1.0, mana_cooldown=1.0)
    action = h.check(health_pct=90.0, mana_pct=80.0)
    assert action is None


def test_healer_respects_cooldown():
    h = Healer(health_threshold=60, mana_threshold=40, health_cooldown=10.0, mana_cooldown=10.0)
    action = h.check(health_pct=30.0, mana_pct=80.0)
    assert action == "health"
    h.mark_used("health")
    action = h.check(health_pct=30.0, mana_pct=80.0)
    assert action is None


def test_healer_health_priority_over_mana():
    h = Healer(health_threshold=60, mana_threshold=40, health_cooldown=1.0, mana_cooldown=1.0)
    action = h.check(health_pct=30.0, mana_pct=20.0)
    assert action == "health"
