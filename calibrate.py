"""Calibration helper for TibiaBot v2.

Run different sub-commands to locate UI regions that must be set in
bot_config.yaml before the bot can function correctly.

Commands
--------
python calibrate.py --show-coords
    Takes a screenshot, highlights the current coord_display region, and
    prints the OCR result.  Adjust coord_display.x/y/width/height until
    the numbers parse correctly.

python calibrate.py --show-bars
    Shows where the HP and mana bars are detected.

python calibrate.py --show-viewport
    Draws the configured viewport boundary and character centre on a
    saved screenshot so you can verify tile_size alignment.

python calibrate.py --dump-frame
    Saves a full screenshot to  calibration_frame.png  for manual inspection.
"""

import argparse
import sys
import time

import cv2
import mss
import numpy as np
import yaml

# ── load config ───────────────────────────────────────────────────────────────

def _load_yaml(path: str = "bot_config.yaml") -> dict:
    try:
        with open(path) as f:
            return yaml.safe_load(f) or {}
    except FileNotFoundError:
        return {}


def _grab_screen(cfg: dict) -> np.ndarray:
    s = cfg.get("screen", {})
    w = s.get("width", 2560)
    h = s.get("height", 1440)
    with mss.mss() as sct:
        raw = sct.grab({"top": 0, "left": 0, "width": w, "height": h})
    return np.array(raw)  # BGRA


# ── commands ─────────────────────────────────────────────────────────────────

def cmd_dump_frame(cfg: dict) -> None:
    frame = _grab_frame(cfg)
    out = "calibration_frame.png"
    cv2.imwrite(out, frame[:, :, :3])
    print(f"Saved → {out}")


def _grab_frame(cfg):
    return _grab_screen(cfg)


def cmd_show_coords(cfg: dict) -> None:
    from bot.vision import read_coordinates_ocr, ocr_available
    if not ocr_available():
        print(
            "pytesseract is not installed.\n"
            "Install with:  pip install pytesseract\n"
            "Also install Tesseract-OCR:  https://github.com/tesseract-ocr/tesseract"
        )
        sys.exit(1)

    cd = cfg.get("coord_display", {})
    x, y = cd.get("x", 1820), cd.get("y", 372)
    w, h = cd.get("width", 180), cd.get("height", 14)

    print(f"Coordinate ROI: x={x} y={y} w={w} h={h}")
    print("Taking screenshot in 2 s – switch to Tibia…")
    time.sleep(2)

    frame = _grab_frame(cfg)
    roi = frame[y : y + h, x : x + w]

    # Save the ROI enlarged for inspection
    enlarged = cv2.resize(roi[:, :, :3], None, fx=5, fy=5, interpolation=cv2.INTER_NEAREST)
    out = "calibration_coords_roi.png"
    cv2.imwrite(out, enlarged)
    print(f"ROI saved (5× zoom) → {out}")

    result = read_coordinates_ocr(roi)
    if result:
        print(f"OCR result: X={result[0]}  Y={result[1]}  Z={result[2]}  ✓")
    else:
        print(
            "OCR could not parse coordinates.\n"
            "Check calibration_coords_roi.png and adjust coord_display in bot_config.yaml."
        )

    # Also save full frame with highlighted region
    vis = frame[:, :, :3].copy()
    cv2.rectangle(vis, (x, y), (x + w, y + h), (0, 255, 0), 2)
    out_full = "calibration_coords_full.png"
    # Save downscaled for easier viewing
    scale = 0.5
    small = cv2.resize(vis, None, fx=scale, fy=scale)
    cv2.imwrite(out_full, small)
    print(f"Full screenshot with highlighted region (50%) → {out_full}")


def cmd_show_bars(cfg: dict) -> None:
    from bot.vision import find_template, read_bar_percent

    print("Taking screenshot in 2 s – switch to Tibia…")
    time.sleep(2)
    frame = _grab_frame(cfg)
    vis = frame[:, :, :3].copy()

    # HP bar
    hp_pos = find_template(frame, "images/health.png")
    if hp_pos:
        row, col = hp_pos
        bx, by = col + 5, row + 7
        hp = read_bar_percent(frame, bx, by, 92, (255, 113, 113))
        cv2.rectangle(vis, (bx, by - 2), (bx + 92, by + 4), (0, 0, 255), 2)
        cv2.putText(vis, f"HP {hp:.0f}%", (bx, by - 6), cv2.FONT_HERSHEY_SIMPLEX, 0.5, (0, 0, 255), 1)
        print(f"HP bar at ({bx},{by})  →  {hp:.0f}%")
    else:
        print("HP bar NOT found – check images/health.png template")

    # Mana bar
    mp_pos = find_template(frame, "images/mana.png")
    if mp_pos:
        row, col = mp_pos
        bx, by = col + 5, row + 6
        mp = read_bar_percent(frame, bx, by, 92, (101, 98, 240))
        cv2.rectangle(vis, (bx, by - 2), (bx + 92, by + 4), (255, 0, 0), 2)
        cv2.putText(vis, f"MP {mp:.0f}%", (bx, by - 6), cv2.FONT_HERSHEY_SIMPLEX, 0.5, (255, 0, 0), 1)
        print(f"Mana bar at ({bx},{by})  →  {mp:.0f}%")
    else:
        print("Mana bar NOT found – check images/mana.png template")

    scale = 0.5
    out = "calibration_bars.png"
    cv2.imwrite(out, cv2.resize(vis, None, fx=scale, fy=scale))
    print(f"Annotated screenshot → {out}")


def cmd_show_viewport(cfg: dict) -> None:
    vp = cfg.get("viewport", {})
    left   = vp.get("left", 0)
    top    = vp.get("top", 0)
    width  = vp.get("width", 1700)
    height = vp.get("height", 1280)
    cx     = vp.get("center_x", 1185)
    cy     = vp.get("center_y", 610)
    ts     = vp.get("tile_size", 64)

    print("Taking screenshot in 2 s – switch to Tibia…")
    time.sleep(2)
    frame = _grab_frame(cfg)
    vis = frame[:, :, :3].copy()

    # Viewport boundary
    cv2.rectangle(vis, (left, top), (left + width, top + height), (0, 255, 0), 2)
    # Character centre
    cv2.drawMarker(vis, (cx, cy), (0, 255, 0), cv2.MARKER_CROSS, 20, 2)
    # Tile grid (every tile_size pixels)
    for gx in range(left, left + width, ts):
        cv2.line(vis, (gx, top), (gx, top + height), (0, 128, 0), 1)
    for gy in range(top, top + height, ts):
        cv2.line(vis, (left, gy), (left + width, gy), (0, 128, 0), 1)

    scale = 0.4
    out = "calibration_viewport.png"
    cv2.imwrite(out, cv2.resize(vis, None, fx=scale, fy=scale))
    print(f"Viewport overlay (40%) → {out}")
    print(f"  Boundary : ({left},{top}) – ({left+width},{top+height})")
    print(f"  Centre   : ({cx},{cy})")
    print(f"  Tile size: {ts}px")


def cmd_show_attack_indicator(cfg: dict) -> None:
    """Visualise the attack indicator pixel so the user can verify / adjust
    attack_indicator_offset in bot_config.yaml.

    Stand in Tibia and START attacking a monster before running this.
    The output image marks:
      • green cross  – enemy-detection pixel (_battle_pixel)
      • red square   – 3×3 attack-indicator region being sampled
    If the red square falls outside the red border, adjust
    attack_indicator_offset until it is inside the border.
    """
    from bot.vision import find_template
    import numpy as np

    c = cfg.get("combat", {})
    r_off, c_off = c.get("attack_indicator_offset", [-10, -10])

    print("Taking screenshot in 2 s – attack a monster in Tibia first…")
    time.sleep(2)
    frame = _grab_frame(cfg)
    vis = frame[:, :, :3].copy()

    bp = find_template(frame, "images/battle.png")
    if not bp:
        print("ERROR: battle.png not found on screen – is Tibia open?")
        return

    brow, bcol = bp[0] + 20, bp[1] + 6
    ind_row, ind_col = brow + r_off, bcol + c_off

    # Sample the indicator region
    region = frame[ind_row : ind_row + 3, ind_col : ind_col + 3, :3]
    red_count = int(np.sum(
        (region[:, :, 2] > 200) & (region[:, :, 1] < 50) & (region[:, :, 0] < 50)
    ))

    # Annotate
    cv2.drawMarker(vis, (bcol, brow), (0, 255, 0), cv2.MARKER_CROSS, 16, 2)
    cv2.rectangle(vis, (ind_col, ind_row), (ind_col + 3, ind_row + 3), (0, 0, 255), 1)

    # Zoom into the battle list area for detailed inspection
    zoom_col = max(0, bcol - 30)
    zoom_row = max(0, brow - 30)
    zoom = vis[zoom_row : zoom_row + 80, zoom_col : zoom_col + 80]
    zoom = cv2.resize(zoom, None, fx=6, fy=6, interpolation=cv2.INTER_NEAREST)

    out_full = "calibration_attack_indicator.png"
    out_zoom = "calibration_attack_indicator_zoom.png"
    cv2.imwrite(out_full, cv2.resize(vis, None, fx=0.5, fy=0.5))
    cv2.imwrite(out_zoom, zoom)

    status = "ATTACKING ✓" if red_count >= 3 else "NOT detected"
    print(f"Enemy pixel   : ({bcol}, {brow})")
    print(f"Indicator     : ({ind_col}, {ind_row})  offset=[{r_off}, {c_off}]")
    print(f"Red pixels    : {red_count}/9  →  {status}")
    print(f"Full screenshot (50%)  → {out_full}")
    print(f"Zoom (6×) around battle list  → {out_zoom}")
    if red_count < 3:
        print("\nHint: adjust attack_indicator_offset in bot_config.yaml so the")
        print("      red square lands on the red border of the selected entry.")


# ── main ──────────────────────────────────────────────────────────────────────

def main() -> None:
    p = argparse.ArgumentParser(description="TibiaBot v2 calibration helper")
    p.add_argument("--config", default="bot_config.yaml")
    p.add_argument("--show-coords",            action="store_true")
    p.add_argument("--show-bars",              action="store_true")
    p.add_argument("--show-viewport",          action="store_true")
    p.add_argument("--show-attack-indicator",  action="store_true")
    p.add_argument("--dump-frame",             action="store_true")
    args = p.parse_args()

    cfg = _load_yaml(args.config)

    if args.show_coords:
        cmd_show_coords(cfg)
    elif args.show_bars:
        cmd_show_bars(cfg)
    elif args.show_viewport:
        cmd_show_viewport(cfg)
    elif args.show_attack_indicator:
        cmd_show_attack_indicator(cfg)
    elif args.dump_frame:
        cmd_dump_frame(cfg)
    else:
        p.print_help()


if __name__ == "__main__":
    main()
