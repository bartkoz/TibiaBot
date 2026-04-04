"""OBS Virtual Camera frame reader using OpenCV."""

from __future__ import annotations

import cv2
import numpy as np


class FrameCapture:
    def __init__(self, camera_index: int = 0):
        self._camera_index = camera_index
        self._cap: cv2.VideoCapture | None = None

    def open(self) -> bool:
        self._cap = cv2.VideoCapture(self._camera_index)
        return self._cap.isOpened()

    def read(self) -> np.ndarray | None:
        if self._cap is None or not self._cap.isOpened():
            return None
        ret, frame = self._cap.read()
        if not ret:
            return None
        return frame

    def crop_region(self, frame: np.ndarray, region: list[int]) -> np.ndarray:
        x, y, w, h = region
        return frame[y : y + h, x : x + w]

    def close(self) -> None:
        if self._cap is not None:
            self._cap.release()
            self._cap = None
