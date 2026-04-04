"""FastAPI web application for cavebot control."""

from __future__ import annotations

import asyncio
import io
import logging
from pathlib import Path

import numpy as np
from fastapi import FastAPI, WebSocket, WebSocketDisconnect
from fastapi.responses import HTMLResponse, StreamingResponse
from fastapi.staticfiles import StaticFiles
from PIL import Image

from cavebot.core.bot import BotStateMachine, CaveBot
from cavebot.core.config import BotConfig, load_config
from cavebot.navigation.atlas import Atlas
from cavebot.web.ws import ConnectionManager

logger = logging.getLogger(__name__)

_STATIC_DIR = Path(__file__).parent / "static"


def create_app(config_path: Path | None = None) -> FastAPI:
    app = FastAPI(title="Tibia Cavebot")
    manager = ConnectionManager()

    if config_path and config_path.exists():
        config = load_config(config_path)
    else:
        config = BotConfig()

    app.state.config = config
    app.state.bot = None
    app.state.atlas = None
    app.state.manager = manager

    @app.get("/", response_class=HTMLResponse)
    async def index():
        index_path = _STATIC_DIR / "index.html"
        if index_path.exists():
            return index_path.read_text()
        return "<html><body><h1>Cavebot</h1><p>Static files not found.</p></body></html>"

    @app.get("/api/config")
    async def get_config():
        return app.state.config.to_dict()

    @app.post("/api/config")
    async def update_config(data: dict):
        return {"status": "ok"}

    @app.post("/api/bot/start")
    async def bot_start():
        config = app.state.config
        if app.state.atlas is None:
            atlas = Atlas(config.minimap.data_path)
            try:
                atlas.load()
            except Exception as e:
                logger.warning(f"Atlas load failed: {e}")
            app.state.atlas = atlas

        bot = CaveBot(config, app.state.atlas)

        async def push_status(status):
            await manager.broadcast(status)

        def sync_push(status):
            try:
                loop = asyncio.get_event_loop()
                if loop.is_running():
                    asyncio.run_coroutine_threadsafe(push_status(status), loop)
            except Exception:
                pass

        bot.on_status_update = sync_push
        bot.start()
        app.state.bot = bot
        return {"status": "started"}

    @app.post("/api/bot/stop")
    async def bot_stop():
        if app.state.bot is not None:
            app.state.bot.stop()
            app.state.bot = None
        return {"status": "stopped"}

    @app.get("/api/bot/status")
    async def bot_status():
        if app.state.bot is not None:
            return app.state.bot.sm.status()
        return {"state": "IDLE", "running": False}

    @app.get("/api/atlas/{z}")
    async def get_atlas_image(z: int):
        atlas = app.state.atlas
        if atlas is None:
            return StreamingResponse(io.BytesIO(b""), media_type="image/png")

        bounds = atlas.world_bounds(z)
        if bounds["max_x"] == 0:
            return StreamingResponse(io.BytesIO(b""), media_type="image/png")

        w = bounds["max_x"] - bounds["min_x"]
        h = bounds["max_y"] - bounds["min_y"]
        canvas = np.zeros((h, w, 3), dtype=np.uint8)

        for key, tile in atlas.color_chunks.items():
            if key[2] != z:
                continue
            ox = key[0] - bounds["min_x"]
            oy = key[1] - bounds["min_y"]
            canvas[oy : oy + 256, ox : ox + 256] = tile

        img = Image.fromarray(canvas)
        buf = io.BytesIO()
        img.save(buf, format="PNG")
        buf.seek(0)
        return StreamingResponse(buf, media_type="image/png")

    @app.get("/api/atlas/bounds/{z}")
    async def get_atlas_bounds(z: int):
        atlas = app.state.atlas
        if atlas is None:
            return {"min_x": 0, "min_y": 0, "max_x": 0, "max_y": 0}
        return atlas.world_bounds(z)

    @app.websocket("/ws")
    async def websocket_endpoint(ws: WebSocket):
        await manager.connect(ws)
        try:
            while True:
                data = await ws.receive_text()
        except WebSocketDisconnect:
            manager.disconnect(ws)

    if _STATIC_DIR.exists():
        app.mount("/static", StaticFiles(directory=str(_STATIC_DIR)), name="static")

    return app
