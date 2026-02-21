"""Interactive waypoint recorder.

Walk your character through the hunting route in Tibia, press the hotkey
to stamp each position, then load the saved JSON into the bot.

Usage
-----
    uv run python record_waypoints.py                        # saves to waypoints/route.json
    uv run python record_waypoints.py waypoints/cave.json    # custom output file
    uv run python record_waypoints.py --load waypoints/cave.json  # continue editing

Controls (work even when Tibia is in focus)
-------------------------------------------
    INSERT      – record current position
    BACKSPACE   – undo last recorded waypoint
    Ctrl+S      – save now  (auto-saved after every addition too)
    Ctrl+L      – list all recorded waypoints
    ESC         – save and quit

Output format (waypoints/route.json)
--------------------------------------
    {
        "name": "route",
        "waypoints": [[32372, 31949, 7], [32380, 31955, 7], ...]
    }

Point bot_config.yaml at the file:
    navigation:
      waypoints_file: "waypoints/route.json"
"""

import argparse
import json
import os
import sys
import threading
import time
from typing import List, Optional, Tuple

import mss
import numpy as np
import yaml

from bot.vision import read_coordinates_ocr, ocr_available

# ── config helpers ────────────────────────────────────────────────────────────

def _load_coord_cfg(config_path: str = "bot_config.yaml") -> dict:
    if not os.path.exists(config_path):
        return {}
    try:
        with open(config_path) as f:
            raw = yaml.safe_load(f) or {}
        return raw.get("coord_display", {})
    except Exception:
        return {}


# ── position reader ───────────────────────────────────────────────────────────

class PositionReader:
    def __init__(self, coord_cfg: dict, screen_cfg: dict) -> None:
        self._cx   = coord_cfg.get("x",      1820)
        self._cy   = coord_cfg.get("y",       372)
        self._cw   = coord_cfg.get("width",   180)
        self._ch   = coord_cfg.get("height",   14)
        self._sw   = screen_cfg.get("width",  2560)
        self._sh   = screen_cfg.get("height", 1440)

    def read(self) -> Optional[Tuple[int, int, int]]:
        monitor = {"top": 0, "left": 0, "width": self._sw, "height": self._sh}
        with mss.mss() as sct:
            frame = np.array(sct.grab(monitor))
        roi = frame[self._cy : self._cy + self._ch, self._cx : self._cx + self._cw]
        return read_coordinates_ocr(roi)


# ── waypoint file I/O ────────────────────────────────────────────────────────

def _save(path: str, name: str, waypoints: List[Tuple[int, int, int]]) -> None:
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "w") as f:
        json.dump({"name": name, "waypoints": [list(wp) for wp in waypoints]}, f, indent=2)


def _load(path: str) -> Tuple[str, List[Tuple[int, int, int]]]:
    with open(path) as f:
        data = json.load(f)
    name = data.get("name", os.path.splitext(os.path.basename(path))[0])
    raw  = data.get("waypoints", [])
    return name, [tuple(int(v) for v in wp) for wp in raw]


# ── recorder ─────────────────────────────────────────────────────────────────

class WaypointRecorder:
    def __init__(self, output_path: str, reader: PositionReader) -> None:
        self.output_path = output_path
        self.reader      = reader
        self.name        = os.path.splitext(os.path.basename(output_path))[0]
        self.waypoints: List[Tuple[int, int, int]] = []
        self._lock = threading.Lock()

    def load_existing(self) -> None:
        if os.path.exists(self.output_path):
            self.name, self.waypoints = _load(self.output_path)
            print(f"Loaded {len(self.waypoints)} existing waypoints from {self.output_path}")

    def record_current(self) -> None:
        pos = self.reader.read()
        if pos is None:
            print("\r[!] Could not read position – is coord_display calibrated?        ")
            return
        with self._lock:
            self.waypoints.append(pos)
            idx = len(self.waypoints)
        self._save_locked()
        print(f"\r[+] #{idx:>3}  ({pos[0]}, {pos[1]}, {pos[2]})  –  saved            ")

    def undo(self) -> None:
        with self._lock:
            if not self.waypoints:
                print("\r[!] Nothing to undo                                            ")
                return
            removed = self.waypoints.pop()
            remaining = len(self.waypoints)
        self._save_locked()
        print(f"\r[-] Removed ({removed[0]}, {removed[1]}, {removed[2]})  –  {remaining} remaining  ")

    def list_all(self) -> None:
        with self._lock:
            wps = list(self.waypoints)
        if not wps:
            print("\r  (no waypoints recorded yet)                                     ")
            return
        print(f"\r  {len(wps)} waypoints:")
        for i, wp in enumerate(wps, 1):
            print(f"    #{i:>3}  ({wp[0]}, {wp[1]}, {wp[2]})")

    def save(self) -> None:
        self._save_locked()
        with self._lock:
            n = len(self.waypoints)
        print(f"\r[✓] Saved {n} waypoints → {self.output_path}                        ")

    def _save_locked(self) -> None:
        with self._lock:
            wps = list(self.waypoints)
        _save(self.output_path, self.name, wps)


# ── live position display ─────────────────────────────────────────────────────

def _live_display(reader: PositionReader, stop: threading.Event) -> None:
    """Background thread: prints current position every second."""
    while not stop.is_set():
        pos = reader.read()
        if pos:
            print(f"\r  current position: ({pos[0]}, {pos[1]}, {pos[2]})  "
                  "[INSERT=record  BKSP=undo  Ctrl+S=save  Ctrl+L=list  ESC=quit]  ",
                  end="", flush=True)
        time.sleep(1.0)


# ── keyboard listener ─────────────────────────────────────────────────────────

def _start_listener(recorder: WaypointRecorder, stop: threading.Event) -> None:
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
                recorder.record_current()

            elif key == keyboard.Key.backspace:
                recorder.undo()

            elif key == keyboard.Key.esc:
                recorder.save()
                print("\nBye!")
                stop.set()
                return False  # stop listener

            elif ctrl_held:
                char = getattr(key, "char", None)
                if char == "s":
                    recorder.save()
                elif char == "l":
                    recorder.list_all()

        except Exception as e:
            print(f"\r[err] {e}                                                       ")

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
    p = argparse.ArgumentParser(description="TibiaBot – waypoint recorder")
    p.add_argument(
        "output",
        nargs="?",
        default="waypoints/route.json",
        help="JSON file to write waypoints to (default: waypoints/route.json)",
    )
    p.add_argument(
        "--load", "-l",
        action="store_true",
        help="Load existing waypoints from the output file and continue recording",
    )
    p.add_argument("--config", default="bot_config.yaml")
    args = p.parse_args()

    if not ocr_available():
        print(
            "pytesseract is not installed or Tesseract-OCR is missing.\n"
            "  macOS : brew install tesseract\n"
            "  Ubuntu: sudo apt install tesseract-ocr\n"
            "  Windows: https://github.com/UB-Mannheim/tesseract/wiki"
        )
        sys.exit(1)

    raw_cfg   = {}
    if os.path.exists(args.config):
        with open(args.config) as f:
            raw_cfg = yaml.safe_load(f) or {}

    reader   = PositionReader(raw_cfg.get("coord_display", {}), raw_cfg.get("screen", {}))
    recorder = WaypointRecorder(args.output, reader)

    if args.load:
        recorder.load_existing()

    print(f"=== Waypoint Recorder ===")
    print(f"  Output file : {args.output}")
    print(f"  Waypoints   : {len(recorder.waypoints)} loaded")
    print()
    print("  Switch to Tibia and walk to your first waypoint.")
    print("  Controls: INSERT=record  BKSP=undo  Ctrl+S=save  Ctrl+L=list  ESC=quit")
    print()

    stop = threading.Event()

    display_thread = threading.Thread(target=_live_display, args=(reader, stop), daemon=True)
    display_thread.start()

    _start_listener(recorder, stop)


if __name__ == "__main__":
    main()
