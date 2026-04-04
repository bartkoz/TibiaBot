import time
from cavebot.combat.fighter import SpellRotation


def test_spell_rotation_ready():
    rotation = SpellRotation(spell_keys=["F4", "F5"], cooldowns=[2.0, 6.0])
    key = rotation.next_ready_spell()
    assert key == "F4"


def test_spell_rotation_cooldown():
    rotation = SpellRotation(spell_keys=["F4", "F5"], cooldowns=[2.0, 6.0])
    rotation.mark_used("F4")
    key = rotation.next_ready_spell()
    assert key == "F5"


def test_spell_rotation_all_on_cooldown():
    rotation = SpellRotation(spell_keys=["F4"], cooldowns=[10.0])
    rotation.mark_used("F4")
    key = rotation.next_ready_spell()
    assert key is None
