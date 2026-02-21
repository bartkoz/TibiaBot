"""Abstract base for all bot modules."""

import asyncio
from abc import ABC, abstractmethod

from bot.screen import ScreenCapture
from bot.state import GameState


class BaseModule(ABC):
    """Every module receives the shared screen and state objects.

    Subclasses implement ``run()`` as an infinite asyncio coroutine.
    They should ``await asyncio.sleep(...)`` frequently so the event loop
    can serve other modules.
    """

    def __init__(self, screen: ScreenCapture, state: GameState) -> None:
        self.screen = screen
        self.state = state

    @abstractmethod
    async def run(self) -> None:
        ...

    async def _wait_for_frame(self) -> None:
        """Yield until the screen capture produces its first frame."""
        while self.screen.get_frame() is None:
            await asyncio.sleep(0.05)
