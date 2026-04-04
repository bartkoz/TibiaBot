import numpy as np

from cavebot.navigation.atlas import Atlas


def test_atlas_load(sample_minimap_dir):
    atlas = Atlas(sample_minimap_dir)
    atlas.load()
    assert len(atlas.color_chunks) == 3
    assert len(atlas.cost_chunks) == 3
    assert (32000, 32000, 7) in atlas.color_chunks
    assert (32256, 32000, 7) in atlas.cost_chunks


def test_atlas_get_color_tile(sample_minimap_dir):
    atlas = Atlas(sample_minimap_dir)
    atlas.load()
    tile = atlas.get_color_tile(32000, 32000, 7)
    assert tile is not None
    assert tile.shape == (256, 256, 3)


def test_atlas_get_cost_at(sample_minimap_dir):
    atlas = Atlas(sample_minimap_dir)
    atlas.load()
    cost = atlas.get_cost_at(32000 + 5, 32000 + 5, 7)
    assert cost == 255
    cost = atlas.get_cost_at(32000 + 100, 32000 + 100, 7)
    assert cost == 150


def test_atlas_get_cost_at_missing_chunk(sample_minimap_dir):
    atlas = Atlas(sample_minimap_dir)
    atlas.load()
    cost = atlas.get_cost_at(99999, 99999, 7)
    assert cost == 255


def test_atlas_z_levels(sample_minimap_dir):
    atlas = Atlas(sample_minimap_dir)
    atlas.load()
    assert 7 in atlas.z_levels


def test_atlas_chunk_keys_for_z(sample_minimap_dir):
    atlas = Atlas(sample_minimap_dir)
    atlas.load()
    keys = atlas.chunk_keys_for_z(7)
    assert len(keys) == 3


def test_atlas_world_bounds(sample_minimap_dir):
    atlas = Atlas(sample_minimap_dir)
    atlas.load()
    bounds = atlas.world_bounds(7)
    assert bounds["min_x"] == 32000
    assert bounds["min_y"] == 32000
    assert bounds["max_x"] == 32256 + 256
    assert bounds["max_y"] == 32256 + 256
