"""Attack and spell rotation via hotkeys."""

from __future__ import annotations

import time

from cavebot.core.input import press_key


class SpellRotation:
    def __init__(self, spell_keys: list[str], cooldowns: list[float]):
        self._spells = list(zip(spell_keys, cooldowns))
        self._last_used: dict[str, float] = {}

    def next_ready_spell(self) -> str | None:
        now = time.monotonic()
        for key, cooldown in self._spells:
            last = self._last_used.get(key, 0.0)
            if now - last >= cooldown:
                return key
        return None

    def mark_used(self, key: str) -> None:
        self._last_used[key] = time.monotonic()

    def cast_next(self) -> bool:
        key = self.next_ready_spell()
        if key is None:
            return False
        press_key(key)
        self.mark_used(key)
        return True


def attack(attack_key: str) -> None:
    press_key(attack_key)
