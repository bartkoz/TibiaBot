"""Keyboard simulation via pynput."""

from __future__ import annotations

import time

from pynput.keyboard import Controller, Key

_keyboard = Controller()

_KEY_MAP: dict[str, Key | str] = {
    "up": Key.up,
    "down": Key.down,
    "left": Key.left,
    "right": Key.right,
    "F1": Key.f1, "F2": Key.f2, "F3": Key.f3, "F4": Key.f4,
    "F5": Key.f5, "F6": Key.f6, "F7": Key.f7, "F8": Key.f8,
    "F9": Key.f9, "F10": Key.f10, "F11": Key.f11, "F12": Key.f12,
}


def press_key(key_name: str) -> None:
    key = _KEY_MAP.get(key_name, key_name)
    _keyboard.press(key)
    _keyboard.release(key)


def press_keys_simultaneous(key_names: list[str]) -> None:
    keys = [_KEY_MAP.get(k, k) for k in key_names]
    for k in keys:
        _keyboard.press(k)
    time.sleep(0.02)
    for k in reversed(keys):
        _keyboard.release(k)
