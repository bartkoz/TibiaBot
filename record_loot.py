"""Interactive loot template recorder.

Open a container in Tibia full of items you want to whitelist, hover over each
item icon, and press INSERT to capture a 32×32 crop.  Templates are saved to
``loot/`` and ``bot_config.yaml`` is updated automatically.

Usage
-----
    uv run python record_loot.py                      # uses loot/ + bot_config.yaml
    uv run python record_loot.py --dir loot/cave      # custom template directory
    uv run python record_loot.py --size 40            # item slot size in px (default 32)
    uv run python record_loot.py --config other.yaml

Controls (work even when Tibia is in focus)
-------------------------------------------
    INSERT      – capture item icon at mouse cursor → prompts for a name
    BACKSPACE   – undo last added item (deletes PNG + removes from whitelist)
    Ctrl+L      – list all templates currently in the output directory
    ESC         – quit
"""

import argparse
import os
import queue
import sys
import threading
import time
from typing import List, Optional

import cv2
import mss
import numpy as np
import pyautogui
import yaml


# ── screen grabber ─────────────────────────────────────────────────────────────

class ScreenGrabber:
    """Grab a size×size pixel crop centred on (x, y) from the primary monitor."""

    def grab(self, x: int, y: int, size: int) -> np.ndarray:
        half = size // 2
        with mss.mss() as sct:
            # monitors[0] is the virtual full-screen; monitors[1] is primary
            mon = sct.monitors[1]
            frame = np.array(sct.grab(mon))  # BGRA

        frame = cv2.cvtColor(frame, cv2.COLOR_BGRA2BGR)

        h, w = frame.shape[:2]
        y1 = max(0, y - half)
        y2 = min(h, y + half)
        x1 = max(0, x - half)
        x2 = min(w, x + half)
        crop = frame[y1:y2, x1:x2]

        # Pad to exact size if we're near a screen edge
        if crop.shape[0] != size or crop.shape[1] != size:
            padded = np.zeros((size, size, 3), dtype=np.uint8)
            padded[: crop.shape[0], : crop.shape[1]] = crop
            crop = padded

        return crop


# ── recorder ──────────────────────────────────────────────────────────────────

class LootRecorder:
    """Manages PNG templates and keeps bot_config.yaml whitelist in sync."""

    def __init__(self, loot_dir: str, config_path: str) -> None:
        self.loot_dir    = loot_dir
        self.config_path = config_path
        self.history: List[str] = []   # names added this session (for undo)
        self._lock = threading.Lock()
        os.makedirs(loot_dir, exist_ok=True)

    # ── public API ────────────────────────────────────────────────────────────

    def add(self, name: str, crop: np.ndarray) -> None:
        """Save the PNG, write a zoomed preview, and update the YAML whitelist."""
        png_path = os.path.join(self.loot_dir, f"{name}.png")
        cv2.imwrite(png_path, crop)

        # 4× zoom preview saved next to this script for quick visual check
        h, w = crop.shape[:2]
        preview = cv2.resize(crop, (w * 4, h * 4), interpolation=cv2.INTER_NEAREST)
        cv2.imwrite("loot_preview.png", preview)

        self._yaml_add(name)

        with self._lock:
            self.history.append(name)

        print(f"\r[+] Saved {png_path}  (whitelist updated, preview → loot_preview.png)          ")

    def undo(self) -> None:
        """Remove the last captured item: deletes PNG and strips from whitelist."""
        with self._lock:
            if not self.history:
                print("\r[!] Nothing to undo                                                          ")
                return
            name = self.history.pop()

        png_path = os.path.join(self.loot_dir, f"{name}.png")
        if os.path.exists(png_path):
            os.remove(png_path)

        self._yaml_remove(name)
        print(f"\r[-] Removed '{name}'  (PNG deleted, whitelist updated)                             ")

    def list_all(self) -> None:
        """Print all PNG templates currently in the output directory."""
        try:
            pngs = sorted(f[:-4] for f in os.listdir(self.loot_dir) if f.endswith(".png"))
        except FileNotFoundError:
            pngs = []
        if not pngs:
            print(f"\r  (no templates in {self.loot_dir} yet)                                        ")
            return
        print(f"\r  {len(pngs)} template(s) in {self.loot_dir}:")
        for n in pngs:
            print(f"    {n}")

    # ── YAML helpers ──────────────────────────────────────────────────────────

    def _load_yaml(self) -> dict:
        if not os.path.exists(self.config_path):
            return {}
        with open(self.config_path) as f:
            return yaml.safe_load(f) or {}

    def _save_yaml(self, raw: dict) -> None:
        with open(self.config_path, "w") as f:
            yaml.dump(raw, f, default_flow_style=False, allow_unicode=True)

    def _yaml_add(self, name: str) -> None:
        raw = self._load_yaml()
        loot_cfg = raw.setdefault("loot", {})
        whitelist: List[str] = loot_cfg.setdefault("whitelist", [])
        if name not in whitelist:
            whitelist.append(name)
        self._save_yaml(raw)

    def _yaml_remove(self, name: str) -> None:
        raw = self._load_yaml()
        whitelist: List[str] = raw.get("loot", {}).get("whitelist", [])
        if name in whitelist:
            whitelist.remove(name)
        self._save_yaml(raw)


# ── live mouse display ────────────────────────────────────────────────────────

def _live_display(stop: threading.Event, input_mode: threading.Event) -> None:
    """Background daemon: refreshes mouse position every 0.5 s."""
    while not stop.is_set():
        if not input_mode.is_set():
            x, y = pyautogui.position()
            print(
                f"\r  mouse: ({x}, {y})  "
                "[INSERT=capture  BKSP=undo  Ctrl+L=list  ESC=quit]  ",
                end="", flush=True,
            )
        time.sleep(0.5)


# ── keyboard listener ─────────────────────────────────────────────────────────

_EVENT_CAPTURE = "capture"
_EVENT_UNDO    = "undo"
_EVENT_LIST    = "list"
_EVENT_QUIT    = "quit"


def _start_listener(event_queue: "queue.Queue[str]", stop: threading.Event) -> None:
    try:
        from pynput import keyboard
    except ImportError:
        print("pynput not installed. Run:  uv sync")
        sys.exit(1)

    ctrl_held = False

    def on_press(key):
        nonlocal ctrl_held
        try:
            if key in (keyboard.Key.ctrl_l, keyboard.Key.ctrl_r):
                ctrl_held = True
                return

            if key == keyboard.Key.insert:
                event_queue.put(_EVENT_CAPTURE)

            elif key == keyboard.Key.backspace:
                event_queue.put(_EVENT_UNDO)

            elif key == keyboard.Key.esc:
                event_queue.put(_EVENT_QUIT)
                return False  # stop listener

            elif ctrl_held:
                char = getattr(key, "char", None)
                if char == "l":
                    event_queue.put(_EVENT_LIST)

        except Exception as e:
            print(f"\r[err] {e}                                                                   ")

    def on_release(key):
        nonlocal ctrl_held
        if key in (keyboard.Key.ctrl_l, keyboard.Key.ctrl_r):
            ctrl_held = False

    listener = keyboard.Listener(on_press=on_press, on_release=on_release)
    listener.start()
    stop.wait()
    listener.stop()


# ── main ──────────────────────────────────────────────────────────────────────

def main() -> None:
    p = argparse.ArgumentParser(description="TibiaBot – loot template recorder")
    p.add_argument(
        "--dir", default="loot",
        help="Directory to save PNG templates in (default: loot)",
    )
    p.add_argument(
        "--size", default=32, type=int,
        help="Capture size in pixels – should match the Tibia item slot width (default: 32)",
    )
    p.add_argument("--config", default="bot_config.yaml")
    args = p.parse_args()

    grabber    = ScreenGrabber()
    recorder   = LootRecorder(args.dir, args.config)
    eq: "queue.Queue[str]" = queue.Queue()
    stop       = threading.Event()
    input_mode = threading.Event()   # set while main thread blocks on input()

    print("=== Loot Template Recorder ===")
    print(f"  Template dir : {args.dir}")
    print(f"  Config file  : {args.config}")
    print(f"  Capture size : {args.size}×{args.size} px")
    print()
    print("  Open a container in Tibia full of items you want to whitelist.")
    print("  Hover over each item icon and press INSERT to capture it.")
    print("  Controls: INSERT=capture  BKSP=undo  Ctrl+L=list  ESC=quit")
    print()

    threading.Thread(
        target=_start_listener, args=(eq, stop), daemon=True,
    ).start()
    threading.Thread(
        target=_live_display, args=(stop, input_mode), daemon=True,
    ).start()

    while not stop.is_set():
        try:
            event = eq.get(timeout=0.1)
        except queue.Empty:
            continue

        if event == _EVENT_QUIT:
            print("\nBye!")
            stop.set()
            break

        elif event == _EVENT_CAPTURE:
            # Snapshot position before pausing for input
            x, y = pyautogui.position()

            input_mode.set()
            time.sleep(0.55)   # let the display thread finish its current line

            print(f"\r  Capturing at ({x}, {y}) …                                                   ")
            print("Name this item: ", end="", flush=True)
            try:
                name = input().strip()
            except EOFError:
                input_mode.clear()
                continue
            finally:
                input_mode.clear()

            if not name:
                print("[!] No name given – capture cancelled")
                continue

            crop = grabber.grab(x, y, args.size)
            recorder.add(name, crop)

        elif event == _EVENT_UNDO:
            recorder.undo()

        elif event == _EVENT_LIST:
            recorder.list_all()


if __name__ == "__main__":
    main()
