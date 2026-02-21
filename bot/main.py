"""Bot entry point.

Usage::

    python -m bot.main                      # run with bot_config.yaml
    python -m bot.main --config my.yaml     # custom config file
    python -m bot.main --debug              # verbose per-module output
"""

import argparse
import asyncio
import signal
import sys

from bot.config import load_config
from bot.modules.combat import CombatModule
from bot.modules.health import HealthModule
from bot.modules.loot import LootModule
from bot.modules.mana import ManaModule
from bot.modules.navigation import NavigationModule
from bot.screen import ScreenCapture
from bot.state import GameState


def _parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="TibiaBot v2")
    p.add_argument("--config", default="bot_config.yaml", help="Path to config file")
    p.add_argument("--debug", action="store_true", help="Extra verbose output")
    return p.parse_args()


async def _run(cfg, debug: bool) -> None:
    screen = ScreenCapture(
        width=cfg.screen.width,
        height=cfg.screen.height,
        fps=cfg.screen.capture_fps,
    )
    state = GameState()

    modules = [
        HealthModule(screen, state, cfg.healing),
        ManaModule(screen, state, cfg.healing),
        CombatModule(screen, state, cfg.combat),
        NavigationModule(screen, state, cfg.navigation, cfg.viewport, cfg.coord_display),
        LootModule(screen, state, cfg.loot, cfg.viewport),
    ]

    screen.start()
    print("Screen capture started – waiting for first frame…")

    if not screen.wait_for_frame(timeout=5.0):
        print("ERROR: No frame received in 5 s. Is Tibia running in the foreground?")
        sys.exit(1)

    print("First frame OK. Starting modules…\n")

    tasks = [asyncio.create_task(m.run(), name=type(m).__name__) for m in modules]

    try:
        await asyncio.gather(*tasks)
    except asyncio.CancelledError:
        pass
    finally:
        state.running = False
        screen.stop()
        for t in tasks:
            t.cancel()


def main() -> None:
    args = _parse_args()
    cfg = load_config(args.config)

    print("=== TibiaBot v2 ===")
    print(f"  Config        : {args.config}")
    print(f"  Resolution    : {cfg.screen.width}×{cfg.screen.height}")
    print(f"  Waypoints     : {len(cfg.navigation.waypoints)}")
    print(f"  Heal key      : {cfg.healing.heal_key} at {cfg.healing.hp_threshold}% HP")
    print(f"  Loot mode     : {'take-all' if '*' in cfg.loot.whitelist else f'{len(cfg.loot.whitelist)} items'}")
    print()

    loop = asyncio.new_event_loop()

    def _stop(sig, frame):
        print(f"\nReceived {signal.Signals(sig).name} – shutting down…")
        loop.stop()

    signal.signal(signal.SIGINT,  _stop)
    signal.signal(signal.SIGTERM, _stop)

    try:
        loop.run_until_complete(_run(cfg, args.debug))
    finally:
        loop.close()


if __name__ == "__main__":
    main()
