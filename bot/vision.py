"""Vision utilities used by all modules.

Single responsibility: turn numpy frames into game-meaningful data.
No side-effects, no state, no pyautogui calls.
"""

import re
from typing import Dict, Optional, Tuple

import cv2
import numpy as np

try:
    import pytesseract
    _OCR_AVAILABLE = True
except ImportError:
    _OCR_AVAILABLE = False

# ── template cache ────────────────────────────────────────────────────────────

_template_cache: Dict[str, Optional[np.ndarray]] = {}


def _load_template(path: str) -> Optional[np.ndarray]:
    if path not in _template_cache:
        tmpl = cv2.imread(path, cv2.IMREAD_UNCHANGED)
        _template_cache[path] = tmpl  # None if file missing - cached to avoid retry spam
    return _template_cache[path]


def find_template(
    frame: np.ndarray,
    template_path: str,
    threshold: float = 0.99,
    method: int = cv2.TM_CCORR_NORMED,
) -> Optional[Tuple[int, int]]:
    """Return (row, col) of the best match, or None if below threshold.

    Frame may be BGRA (4-channel) or BGR (3-channel); alpha is stripped before
    matching.  Template is loaded once and cached.
    """
    template = _load_template(template_path)
    if template is None:
        return None

    # Strip alpha channels so shapes always match
    f = frame[:, :, :3] if frame.ndim == 3 and frame.shape[2] == 4 else frame
    t = template[:, :, :3] if template.ndim == 3 and template.shape[2] == 4 else template

    result = cv2.matchTemplate(f, t, method)
    _, max_val, _, max_loc = cv2.minMaxLoc(result)
    if max_val < threshold:
        return None
    col, row = max_loc  # minMaxLoc returns (x, y) = (col, row)
    return row, col


# ── pixel helpers ─────────────────────────────────────────────────────────────

def pixel_rgb(frame: np.ndarray, x: int, y: int) -> Tuple[int, int, int]:
    """Return the (R, G, B) value at screen pixel (x, y).  Frame is BGRA."""
    b, g, r = int(frame[y, x, 0]), int(frame[y, x, 1]), int(frame[y, x, 2])
    return r, g, b


# ── resource bar reading ──────────────────────────────────────────────────────

def read_bar_percent(
    frame: np.ndarray,
    bar_left: int,
    bar_y: int,
    bar_width: int,
    color_rgb: Tuple[int, int, int],
    tolerance: int = 12,
) -> float:
    """Return fill level of a solid-color resource bar as 0–100 %.

    Scans pixel-by-pixel from right to left, returning the position of the
    rightmost pixel that matches *color_rgb*.  This gives a continuous reading
    instead of the original 10 % snap.

    Args:
        frame:      BGRA screen capture.
        bar_left:   X pixel of the bar's left edge.
        bar_y:      Y pixel of the bar's centre row.
        bar_width:  Total pixel width of the bar at 100 %.
        color_rgb:  Expected (R, G, B) of the filled portion.
        tolerance:  Per-channel tolerance for the colour match.
    """
    r_exp, g_exp, b_exp = color_rgb
    # Extract the bar row (BGR order in numpy)
    row = frame[bar_y, bar_left : bar_left + bar_width, :3].astype(np.int32)

    for i in range(bar_width - 1, -1, -1):
        b, g, r = row[i]
        if abs(r - r_exp) <= tolerance and abs(g - g_exp) <= tolerance and abs(b - b_exp) <= tolerance:
            return round((i / bar_width) * 100, 1)
    return 0.0


# ── coordinate OCR ────────────────────────────────────────────────────────────

def read_coordinates_ocr(roi: np.ndarray) -> Optional[Tuple[int, int, int]]:
    """Extract (X, Y, Z) world coordinates from a minimap coordinate ROI.

    Tibia renders coordinates as white text on a dark background directly
    below the minimap panel, e.g. ``32372, 31949, 7``.

    Requires pytesseract + Tesseract to be installed.  Returns None if
    pytesseract is unavailable or the text can't be parsed.
    """
    if not _OCR_AVAILABLE:
        return None

    # Convert to BGR if BGRA
    if roi.ndim == 3 and roi.shape[2] == 4:
        roi = roi[:, :, :3]

    gray = cv2.cvtColor(roi, cv2.COLOR_BGR2GRAY)

    # Scale up 3× — Tesseract performs better on larger text
    gray = cv2.resize(gray, None, fx=3, fy=3, interpolation=cv2.INTER_NEAREST)

    # Threshold: white text on dark → invert so digits are black on white
    _, binary = cv2.threshold(gray, 100, 255, cv2.THRESH_BINARY)

    text = pytesseract.image_to_string(
        binary,
        config="--psm 7 --oem 3 -c tessedit_char_whitelist=0123456789, ",
    ).strip()

    # Accept "32372, 31949, 7" or "32372,31949,7" or spacing variants
    match = re.search(r"(\d{3,6})[,\s]+(\d{3,6})[,\s]+(\d{1,2})", text)
    if match:
        return int(match.group(1)), int(match.group(2)), int(match.group(3))
    return None


def ocr_available() -> bool:
    return _OCR_AVAILABLE
