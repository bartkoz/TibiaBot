"""Background screen capture thread.

A single thread captures the screen at a fixed FPS and stores the latest
frame in shared memory.  Every module reads from this shared frame instead
of issuing its own screen-grab, cutting redundant I/O from O(N modules) to
O(1) per tick.
"""

import threading
import time
from typing import Optional

import mss
import numpy as np


class ScreenCapture:
    """Captures the full screen in a background daemon thread.

    Usage::

        sc = ScreenCapture(width=2560, height=1440, fps=20)
        sc.start()
        sc.wait_for_frame()
        frame = sc.get_frame()   # numpy BGRA array, updated ~20 Hz
        roi   = sc.get_roi(x, y, w, h)
    """

    def __init__(self, width: int, height: int, fps: int = 20):
        self._width = width
        self._height = height
        self._interval = 1.0 / fps
        self._frame: Optional[np.ndarray] = None
        self._lock = threading.RLock()
        self._stop = threading.Event()
        self._thread = threading.Thread(
            target=self._capture_loop, daemon=True, name="ScreenCapture"
        )

    # ── lifecycle ────────────────────────────────────────────────────────────

    def start(self) -> None:
        self._thread.start()

    def stop(self) -> None:
        self._stop.set()

    # ── frame access ─────────────────────────────────────────────────────────

    def get_frame(self) -> Optional[np.ndarray]:
        """Return the latest captured frame (BGRA, shape H×W×4)."""
        with self._lock:
            return self._frame

    def get_roi(self, x: int, y: int, width: int, height: int) -> Optional[np.ndarray]:
        """Return a copy of a rectangular region from the latest frame."""
        frame = self.get_frame()
        if frame is None:
            return None
        return frame[y : y + height, x : x + width].copy()

    def wait_for_frame(self, timeout: float = 5.0) -> bool:
        """Block until at least one frame has been captured."""
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if self.get_frame() is not None:
                return True
            time.sleep(0.05)
        return False

    # ── private ──────────────────────────────────────────────────────────────

    def _capture_loop(self) -> None:
        monitor = {
            "top": 0,
            "left": 0,
            "width": self._width,
            "height": self._height,
        }
        with mss.mss() as sct:
            while not self._stop.is_set():
                t0 = time.perf_counter()
                raw = sct.grab(monitor)
                frame = np.array(raw)  # BGRA
                with self._lock:
                    self._frame = frame
                elapsed = time.perf_counter() - t0
                remaining = self._interval - elapsed
                if remaining > 0:
                    time.sleep(remaining)
