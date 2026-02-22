"""Bot entry point.

Usage::

    python -m bot.main                      # run with bot_config.yaml
    python -m bot.main --config my.yaml     # custom config file
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
from bot.modules.minimap_navigation import MinimapNavigationModule
from bot.screen import ScreenCapture
from bot.state import GameState


def _parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="TibiaBot v2")
    p.add_argument("--config", default="bot_config.yaml", help="Path to config file")
    return p.parse_args()


async def _run(cfg, stop_event: asyncio.Event) -> None:
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
        LootModule(screen, state, cfg.loot, cfg.viewport),
    ]
    if cfg.minimap.enabled:
        modules.append(MinimapNavigationModule(screen, state, cfg.minimap, cfg.viewport))
    elif cfg.navigation.enabled:
        modules.append(NavigationModule(screen, state, cfg.navigation, cfg.viewport, cfg.coord_display))

    screen.start()
    print("Screen capture started – waiting for first frame…")

    if not screen.wait_for_frame(timeout=5.0):
        print("ERROR: No frame received in 5 s. Is Tibia running in the foreground?")
        sys.exit(1)

    print("First frame OK. Starting modules…\n")

    tasks = [asyncio.create_task(m.run(), name=type(m).__name__) for m in modules]

    try:
        await stop_event.wait()
    finally:
        print("\nShutting down…")
        state.running = False
        screen.stop()
        for t in tasks:
            t.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)


def main() -> None:
    args = _parse_args()
    cfg = load_config(args.config)

    print("=== TibiaBot v2 ===")
    print(f"  Config        : {args.config}")
    print(f"  Resolution    : {cfg.screen.width}×{cfg.screen.height}")
    if cfg.minimap.enabled:
        nav_status = f"minimap visual ({cfg.minimap.waypoints_file or 'no file set'})"
    elif cfg.navigation.enabled:
        nav_status = f"OCR coordinate ({len(cfg.navigation.waypoints)} waypoints)"
    else:
        nav_status = "disabled"
    print(f"  Navigation    : {nav_status}")
    print(f"  Heal key      : {cfg.healing.heal_key} at {cfg.healing.hp_threshold}% HP")
    loot_mode = "take-all" if "*" in cfg.loot.whitelist else f"{len(cfg.loot.whitelist)} items"
    print(f"  Loot mode     : {loot_mode}")
    print()

    async def _entry() -> None:
        stop_event = asyncio.Event()
        loop = asyncio.get_running_loop()

        def _on_stop() -> None:
            print(f"\nStop signal received – shutting down…")
            stop_event.set()

        for sig in (signal.SIGINT, signal.SIGTERM):
            try:
                loop.add_signal_handler(sig, _on_stop)
            except NotImplementedError:
                # Windows does not support add_signal_handler
                signal.signal(sig, lambda *_: stop_event.set())

        await _run(cfg, stop_event)

    asyncio.run(_entry())


if __name__ == "__main__":
    main()
