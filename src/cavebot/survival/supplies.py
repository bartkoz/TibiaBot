"""Supply tracking and refill logic."""

from __future__ import annotations


class SupplyTracker:
    def __init__(self, max_potions: int, refill_threshold: int):
        self._max = max_potions
        self._threshold = refill_threshold
        self._remaining = max_potions

    @property
    def remaining(self) -> int:
        return self._remaining

    def use_potion(self) -> None:
        if self._remaining > 0:
            self._remaining -= 1

    def needs_refill(self) -> bool:
        return self._remaining <= self._threshold

    def refill(self) -> None:
        self._remaining = self._max
