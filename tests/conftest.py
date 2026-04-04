import numpy as np
import pytest
from PIL import Image


@pytest.fixture
def sample_minimap_dir(tmp_path):
    """Create a minimal set of minimap files for testing."""
    for chunk_x, chunk_y in [(32000, 32000), (32256, 32000), (32000, 32256)]:
        rng = np.random.RandomState(seed=chunk_x + chunk_y)
        color_data = rng.randint(0, 256, (256, 256, 3), dtype=np.uint8)
        img = Image.fromarray(color_data, "RGB").convert("P")
        img.save(tmp_path / f"Minimap_Color_{chunk_x}_{chunk_y}_7.png")

        cost_data = np.full((256, 256), 150, dtype=np.uint8)
        cost_data[0:10, :] = 255
        cost_data[:, 0:10] = 255
        cost_img = Image.fromarray(cost_data, "L")
        cost_img.save(tmp_path / f"Minimap_WaypointCost_{chunk_x}_{chunk_y}_7.png")

    return tmp_path
