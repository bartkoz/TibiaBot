from cavebot.navigation.walker import path_to_directions, Direction


def test_path_to_directions_straight_east():
    path = [(100, 100), (101, 100), (102, 100)]
    dirs = path_to_directions(path)
    assert dirs == [Direction.EAST, Direction.EAST]


def test_path_to_directions_diagonal():
    path = [(100, 100), (101, 101)]
    dirs = path_to_directions(path)
    assert dirs == [Direction.SOUTHEAST]


def test_path_to_directions_north():
    path = [(100, 100), (100, 99)]
    dirs = path_to_directions(path)
    assert dirs == [Direction.NORTH]


def test_path_to_directions_empty():
    path = [(100, 100)]
    dirs = path_to_directions(path)
    assert dirs == []


def test_direction_to_keys():
    assert Direction.NORTH.keys == ["up"]
    assert Direction.SOUTHEAST.keys == ["down", "right"]
    assert Direction.WEST.keys == ["left"]
