"""Interactive minimap-waypoint recorder.

Stand at each patrol point in Tibia and press INSERT.  A greyscale crop of
the minimap (centred on the player dot) is saved as a PNG template and added
to the route JSON.

No OCR or coordinate display is required – navigation is driven entirely by
visual template matching on the live minimap feed.

Usage
-----
    uv run python record_minimap_waypoints.py                        # waypoints/minimap_route.json
    uv run python record_minimap_waypoints.py waypoints/cave.json    # custom output file
    uv run python record_minimap_waypoints.py --load waypoints/cave.json  # continue existing

Controls (global – work while Tibia is focused)
------------------------------------------------
    INSERT      – record current minimap as next waypoint
    BACKSPACE   – remove last recorded waypoint
    Ctrl+S      – save now  (auto-saved after every change)
    Ctrl+L      – list recorded waypoints
    ESC         – save and quit

Output (waypoints/minimap_route.json)
--------------------------------------
    {
        "name": "route",
        "waypoints": [
            "waypoints/minimap/wp_0.png",
            "waypoints/minimap/wp_1.png",
            ...
        ]
    }

Then in bot_config.yaml:
    minimap:
      enabled: true
      waypoints_file: "waypoints/minimap_route.json"
"""

import argparse
import json
import os
import sys
import threading
import time

import cv2
import mss
import numpy as np
import yaml

# Radius of the player-dot area to blank in each template (pixels)
_DOT_RADIUS = 4


# ── config helpers ─────────────────────────────────────────────────────────────

def _load_minimap_cfg(config_path: str = "bot_config.yaml") -> dict:
    if not os.path.exists(config_path):
        return {}
    try:
        with open(config_path) as f:
            raw = yaml.safe_load(f) or {}
        return raw.get("minimap", {})
    except Exception:
        return {}


# ── minimap grabber ────────────────────────────────────────────────────────────

class MinimapGrabber:
    """Captures the minimap region from the screen and returns a template crop."""

    def __init__(self, mm: dict, screen_cfg: dict) -> None:
        self._x  = mm.get("x",     2381)
        self._y  = mm.get("y",        1)
        self._w  = mm.get("width",    86)
        self._h  = mm.get("height",  104)
        self._ts = mm.get("template_size", 40)
        self._sw = screen_cfg.get("width",  2560)
        self._sh = screen_cfg.get("height", 1440)

    def grab_template(self) -> np.ndarray:
        """Return a greyscale template_size×template_size crop of the minimap."""
        monitor = {"top": 0, "left": 0, "width": self._sw, "height": self._sh}
        with mss.mss() as sct:
            frame = np.array(sct.grab(monitor))   # BGRA

        frame_bgr = cv2.cvtColor(frame, cv2.COLOR_BGRA2BGR)
        roi = frame_bgr[self._y : self._y + self._h, self._x : self._x + self._w]
        gray = cv2.cvtColor(roi, cv2.COLOR_BGR2GRAY)

        # Centre crop of template_size × template_size
        cy = gray.shape[0] // 2
        cx = gray.shape[1] // 2
        half = self._ts // 2
        tmpl = gray[cy - half : cy + half, cx - half : cx + half]

        # Blank the player dot (always white cross at centre) so it does not
        # skew template matching when the player moves away from this location
        tc = self._ts // 2
        cv2.circle(tmpl, (tc, tc), _DOT_RADIUS, int(np.median(tmpl)), -1)

        return tmpl

    def grab_preview(self) -> np.ndarray:
        """Return the full minimap BGR image for visual confirmation."""
        monitor = {"top": 0, "left": 0, "width": self._sw, "height": self._sh}
        with mss.mss() as sct:
            frame = np.array(sct.grab(monitor))
        frame_bgr = cv2.cvtColor(frame, cv2.COLOR_BGRA2BGR)
        return frame_bgr[self._y : self._y + self._h, self._x : self._x + self._w]


# ── route I/O ─────────────────────────────────────────────────────────────────

def _save(output_path: str, name: str, wp_paths: list) -> None:
    os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)
    with open(output_path, "w") as f:
        json.dump({"name": name, "waypoints": wp_paths}, f, indent=2)


def _load(output_path: str):
    with open(output_path) as f:
        data = json.load(f)
    name = data.get("name", os.path.splitext(os.path.basename(output_path))[0])
    return name, data.get("waypoints", [])


def _template_dir(output_path: str) -> str:
    """Directory where template PNGs live (sibling of the JSON file)."""
    base = os.path.splitext(output_path)[0]
    return base + "_templates"


# ── recorder ──────────────────────────────────────────────────────────────────

class MinimapWaypointRecorder:
    def __init__(self, output_path: str, grabber: MinimapGrabber) -> None:
        self.output_path = output_path
        self.grabber     = grabber
        self.name        = os.path.splitext(os.path.basename(output_path))[0]
        self.wp_paths: list = []
        self._lock       = threading.Lock()

    def load_existing(self) -> None:
        if os.path.exists(self.output_path):
            self.name, self.wp_paths = _load(self.output_path)
            print(f"Loaded {len(self.wp_paths)} existing waypoints from {self.output_path}")

    def record_current(self) -> None:
        tmpl = self.grabber.grab_template()
        with self._lock:
            idx = len(self.wp_paths)

        tmpl_dir  = _template_dir(self.output_path)
        os.makedirs(tmpl_dir, exist_ok=True)
        png_path = os.path.join(tmpl_dir, f"wp_{idx}.png")
        cv2.imwrite(png_path, tmpl)

        # Also save a 4× zoomed preview for quick visual check
        preview = self.grabber.grab_preview()
        prev_zoom = cv2.resize(preview, None, fx=4, fy=4, interpolation=cv2.INTER_NEAREST)
        cv2.imwrite("minimap_wp_preview.png", prev_zoom)

        with self._lock:
            self.wp_paths.append(png_path)
        self._save_locked()
        print(f"\r[+] #{idx}  saved {png_path}  (preview → minimap_wp_preview.png)            ")

    def undo(self) -> None:
        with self._lock:
            if not self.wp_paths:
                print("\r[!] Nothing to undo                                                   ")
                return
            removed = self.wp_paths.pop()

        if os.path.exists(removed):
            os.remove(removed)
        self._save_locked()
        print(f"\r[-] Removed {removed}  –  {len(self.wp_paths)} remaining                    ")

    def list_all(self) -> None:
        with self._lock:
            paths = list(self.wp_paths)
        if not paths:
            print("\r  (no waypoints recorded yet)                                             ")
            return
        print(f"\r  {len(paths)} waypoints:")
        for i, p in enumerate(paths):
            print(f"    #{i}  {p}")

    def save(self) -> None:
        self._save_locked()
        with self._lock:
            n = len(self.wp_paths)
        print(f"\r[✓] Saved {n} waypoints → {self.output_path}                                ")

    def _save_locked(self) -> None:
        with self._lock:
            paths = list(self.wp_paths)
        _save(self.output_path, self.name, paths)


# ── live display ──────────────────────────────────────────────────────────────

def _live_display(recorder: MinimapWaypointRecorder, stop: threading.Event) -> None:
    while not stop.is_set():
        with recorder._lock:
            n = len(recorder.wp_paths)
        print(
            f"\r  waypoints recorded: {n}  "
            "[INSERT=record  BKSP=undo  Ctrl+S=save  Ctrl+L=list  ESC=quit]  ",
            end="", flush=True,
        )
        time.sleep(1.0)


# ── keyboard listener ─────────────────────────────────────────────────────────

def _start_listener(recorder: MinimapWaypointRecorder, stop: threading.Event) -> None:
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
                return False

            elif ctrl_held:
                char = getattr(key, "char", None)
                if char == "s":
                    recorder.save()
                elif char == "l":
                    recorder.list_all()

        except Exception as e:
            print(f"\r[err] {e}                                                             ")

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
    p = argparse.ArgumentParser(description="TibiaBot – minimap waypoint recorder")
    p.add_argument(
        "output",
        nargs="?",
        default="waypoints/minimap_route.json",
        help="JSON file to write waypoints to (default: waypoints/minimap_route.json)",
    )
    p.add_argument("--load", "-l", action="store_true",
                   help="Load existing waypoints and continue recording")
    p.add_argument("--config", default="bot_config.yaml")
    args = p.parse_args()

    raw_cfg = {}
    if os.path.exists(args.config):
        with open(args.config) as f:
            raw_cfg = yaml.safe_load(f) or {}

    grabber  = MinimapGrabber(raw_cfg.get("minimap", {}), raw_cfg.get("screen", {}))
    recorder = MinimapWaypointRecorder(args.output, grabber)

    if args.load:
        recorder.load_existing()

    print("=== Minimap Waypoint Recorder ===")
    print(f"  Output file  : {args.output}")
    print(f"  Waypoints    : {len(recorder.wp_paths)} loaded")
    print()
    print("  Walk to each patrol point in Tibia and press INSERT.")
    print("  A minimap snapshot is saved as a navigation template.")
    print("  Controls: INSERT=record  BKSP=undo  Ctrl+S=save  Ctrl+L=list  ESC=quit")
    print()
    print("  Tip: verify minimap region first with:")
    print("       uv run python calibrate.py --show-minimap")
    print()

    stop = threading.Event()

    threading.Thread(target=_live_display, args=(recorder, stop), daemon=True).start()
    _start_listener(recorder, stop)


if __name__ == "__main__":
    main()
