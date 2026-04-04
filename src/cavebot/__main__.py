"""CLI entry point: python -m cavebot [--config path] [--port N]."""

from __future__ import annotations

import argparse
import logging
from pathlib import Path

import uvicorn

from cavebot.web.app import create_app


def main() -> None:
    parser = argparse.ArgumentParser(description="Tibia Cavebot")
    parser.add_argument(
        "--config", type=Path, default=Path("configs/default.yaml"),
        help="Path to config YAML file",
    )
    parser.add_argument("--port", type=int, default=8080, help="Web UI port")
    parser.add_argument("--host", default="127.0.0.1", help="Web UI host")
    parser.add_argument("--log-level", default="INFO", help="Logging level")
    args = parser.parse_args()

    logging.basicConfig(
        level=getattr(logging, args.log_level.upper()),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )

    app = create_app(config_path=args.config)
    uvicorn.run(app, host=args.host, port=args.port)


if __name__ == "__main__":
    main()
