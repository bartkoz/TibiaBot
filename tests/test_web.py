import pytest
from httpx import ASGITransport, AsyncClient

from cavebot.web.app import create_app


@pytest.fixture
def app():
    return create_app()


@pytest.mark.asyncio
async def test_index(app):
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get("/")
        assert resp.status_code == 200
        assert "text/html" in resp.headers["content-type"]


@pytest.mark.asyncio
async def test_get_config(app):
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get("/api/config")
        assert resp.status_code == 200
        data = resp.json()
        assert "capture" in data
        assert "minimap" in data


@pytest.mark.asyncio
async def test_bot_start_stop(app):
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post("/api/bot/start")
        assert resp.status_code == 200
        resp = await client.post("/api/bot/stop")
        assert resp.status_code == 200
