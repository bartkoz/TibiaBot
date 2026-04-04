import numpy as np
import pytest
from PIL import Image

from cavebot.navigation.atlas import Atlas
from cavebot.vision.minimap import MinimapLocator


@pytest.fixture
def locator_atlas(tmp_path):
    for chunk_x, chunk_y in [(32000, 32000), (32256, 32000), (32000, 32256)]:
        seed = chunk_x * 1000 + chunk_y
        r = np.random.RandomState(seed=seed)
        color_data = r.randint(0, 256, (256, 256, 3), dtype=np.uint8)
        img = Image.fromarray(color_data, "RGB")
        img.save(tmp_path / f"Minimap_Color_{chunk_x}_{chunk_y}_7.png")
        cost = np.full((256, 256), 150, dtype=np.uint8)
        Image.fromarray(cost, "L").save(
            tmp_path / f"Minimap_WaypointCost_{chunk_x}_{chunk_y}_7.png"
        )
    atlas = Atlas(tmp_path)
    atlas.load()
    return atlas


def test_locator_find_position(locator_atlas):
    locator = MinimapLocator(locator_atlas)
    tile = locator_atlas.get_color_tile(32000, 32000, 7)
    snippet = tile[80:130, 80:130].copy()
    result = locator.locate(snippet, z=7)
    assert result is not None
    x, y, confidence = result
    assert abs(x - (32000 + 80)) <= 2
    assert abs(y - (32000 + 80)) <= 2
    assert confidence > 0.7


def test_locator_narrowed_search(locator_atlas):
    locator = MinimapLocator(locator_atlas)
    tile = locator_atlas.get_color_tile(32000, 32000, 7)
    snippet = tile[100:150, 100:150].copy()
    locator.locate(snippet, z=7)
    snippet2 = tile[105:155, 105:155].copy()
    result = locator.locate(snippet2, z=7)
    assert result is not None
    x, y, confidence = result
    assert abs(x - (32000 + 105)) <= 2
    assert abs(y - (32000 + 105)) <= 2


def test_locator_no_match(locator_atlas):
    locator = MinimapLocator(locator_atlas)
    snippet = np.full((50, 50, 3), 128, dtype=np.uint8)
    result = locator.locate(snippet, z=7, confidence_threshold=0.99)
    assert result is None
