"""A* pathfinder on the WaypointCost grid."""

from __future__ import annotations

import heapq
import math

from cavebot.navigation.atlas import Atlas

_DIRECTIONS = [
    (0, -1, 1.0),
    (0, 1, 1.0),
    (-1, 0, 1.0),
    (1, 0, 1.0),
    (-1, -1, 1.414),
    (1, -1, 1.414),
    (-1, 1, 1.414),
    (1, 1, 1.414),
]

_BLOCKED_COST = 255
_UNEXPLORED_COST = 0


def _octile_distance(x1: int, y1: int, x2: int, y2: int) -> float:
    dx = abs(x2 - x1)
    dy = abs(y2 - y1)
    return max(dx, dy) + (math.sqrt(2) - 1) * min(dx, dy)


def find_path(
    atlas: Atlas,
    start: tuple[int, int, int],
    goal: tuple[int, int, int],
    max_iterations: int = 100_000,
) -> list[tuple[int, int]] | None:
    sx, sy, sz = start
    gx, gy, gz = goal

    if sz != gz:
        return None

    z = sz

    start_cost = atlas.get_cost_at(sx, sy, z)
    goal_cost = atlas.get_cost_at(gx, gy, z)
    if start_cost >= _BLOCKED_COST or start_cost == _UNEXPLORED_COST:
        return None
    if goal_cost >= _BLOCKED_COST or goal_cost == _UNEXPLORED_COST:
        return None

    if sx == gx and sy == gy:
        return [(sx, sy)]

    counter = 0
    open_set: list[tuple[float, int, int, int]] = []
    heapq.heappush(open_set, (_octile_distance(sx, sy, gx, gy), counter, sx, sy))
    counter += 1

    came_from: dict[tuple[int, int], tuple[int, int]] = {}
    g_score: dict[tuple[int, int], float] = {(sx, sy): 0.0}

    iterations = 0
    while open_set and iterations < max_iterations:
        iterations += 1
        _, _, cx, cy = heapq.heappop(open_set)

        if cx == gx and cy == gy:
            path = [(cx, cy)]
            while (cx, cy) in came_from:
                cx, cy = came_from[(cx, cy)]
                path.append((cx, cy))
            path.reverse()
            return path

        for dx, dy, move_cost_mult in _DIRECTIONS:
            nx, ny = cx + dx, cy + dy
            tile_cost = atlas.get_cost_at(nx, ny, z)
            if tile_cost >= _BLOCKED_COST or tile_cost == _UNEXPLORED_COST:
                continue

            move_cost = move_cost_mult * (tile_cost / 150.0)
            tentative_g = g_score[(cx, cy)] + move_cost

            if tentative_g < g_score.get((nx, ny), float("inf")):
                came_from[(nx, ny)] = (cx, cy)
                g_score[(nx, ny)] = tentative_g
                f = tentative_g + _octile_distance(nx, ny, gx, gy)
                heapq.heappush(open_set, (f, counter, nx, ny))
                counter += 1

    return None
