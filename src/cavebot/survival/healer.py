"""Health/mana threshold monitoring and healing."""

from __future__ import annotations

import time


class Healer:
    def __init__(
        self,
        health_threshold: float,
        mana_threshold: float,
        health_cooldown: float,
        mana_cooldown: float,
    ):
        self._health_threshold = health_threshold
        self._mana_threshold = mana_threshold
        self._cooldowns = {"health": health_cooldown, "mana": mana_cooldown}
        self._last_used: dict[str, float] = {}

    def check(self, health_pct: float, mana_pct: float) -> str | None:
        now = time.monotonic()
        if health_pct < self._health_threshold:
            last = self._last_used.get("health", 0.0)
            if now - last >= self._cooldowns["health"]:
                return "health"
        if mana_pct < self._mana_threshold:
            last = self._last_used.get("mana", 0.0)
            if now - last >= self._cooldowns["mana"]:
                return "mana"
        return None

    def mark_used(self, action: str) -> None:
        self._last_used[action] = time.monotonic()
