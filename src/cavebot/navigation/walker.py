"""Convert A* paths to arrow key sequences and execute them."""

from __future__ import annotations

import enum
import time

from cavebot.core.input import press_keys_simultaneous


class Direction(enum.Enum):
    NORTH = (0, -1)
    SOUTH = (0, 1)
    WEST = (-1, 0)
    EAST = (1, 0)
    NORTHWEST = (-1, -1)
    NORTHEAST = (1, -1)
    SOUTHWEST = (-1, 1)
    SOUTHEAST = (1, 1)

    @property
    def keys(self) -> list[str]:
        dx, dy = self.value
        result = []
        if dy < 0:
            result.append("up")
        elif dy > 0:
            result.append("down")
        if dx < 0:
            result.append("left")
        elif dx > 0:
            result.append("right")
        return result


_DELTA_TO_DIR = {d.value: d for d in Direction}


def path_to_directions(path: list[tuple[int, int]]) -> list[Direction]:
    directions = []
    for i in range(1, len(path)):
        dx = path[i][0] - path[i - 1][0]
        dy = path[i][1] - path[i - 1][1]
        dx = max(-1, min(1, dx))
        dy = max(-1, min(1, dy))
        direction = _DELTA_TO_DIR.get((dx, dy))
        if direction is not None:
            directions.append(direction)
    return directions


def walk_path(path: list[tuple[int, int]], step_delay: float = 0.2) -> None:
    directions = path_to_directions(path)
    for direction in directions:
        press_keys_simultaneous(direction.keys)
        time.sleep(step_delay)
