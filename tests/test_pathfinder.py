import numpy as np
import pytest
from PIL import Image

from cavebot.navigation.atlas import Atlas
from cavebot.navigation.pathfinder import find_path


@pytest.fixture
def pathfinding_atlas(tmp_path):
    cost = np.full((256, 256), 255, dtype=np.uint8)
    cost[50:200, 50:200] = 150
    cost_img = Image.fromarray(cost, "L")
    cost_img.save(tmp_path / "Minimap_WaypointCost_32000_32000_7.png")
    color = np.zeros((256, 256, 3), dtype=np.uint8)
    Image.fromarray(color, "RGB").convert("P").save(
        tmp_path / "Minimap_Color_32000_32000_7.png"
    )
    atlas = Atlas(tmp_path)
    atlas.load()
    return atlas


def test_find_path_simple(pathfinding_atlas):
    start = (32000 + 60, 32000 + 60, 7)
    goal = (32000 + 190, 32000 + 190, 7)
    path = find_path(pathfinding_atlas, start, goal)
    assert path is not None
    assert len(path) > 0
    assert path[0] == (start[0], start[1])
    assert path[-1] == (goal[0], goal[1])


def test_find_path_blocked(pathfinding_atlas):
    start = (32000 + 60, 32000 + 60, 7)
    goal = (32000 + 10, 32000 + 10, 7)
    path = find_path(pathfinding_atlas, start, goal)
    assert path is None


def test_find_path_adjacent(pathfinding_atlas):
    start = (32000 + 100, 32000 + 100, 7)
    goal = (32000 + 101, 32000 + 101, 7)
    path = find_path(pathfinding_atlas, start, goal)
    assert path is not None
    assert len(path) == 2


def test_find_path_same_position(pathfinding_atlas):
    start = (32000 + 100, 32000 + 100, 7)
    path = find_path(pathfinding_atlas, start, start)
    assert path is not None
    assert len(path) == 1


def test_find_path_uses_diagonal(pathfinding_atlas):
    start = (32000 + 60, 32000 + 60, 7)
    goal = (32000 + 70, 32000 + 70, 7)
    path = find_path(pathfinding_atlas, start, goal)
    assert path is not None
    assert len(path) <= 12
