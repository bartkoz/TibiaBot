import settings
import pyautogui
from image_grab import detect
from random import randint
from time import sleep


class JunkRemover:

    def __init__(self):
        pos = detect('images/medicinepouch.png')
        self.x = settings.X_MIDDLE
        self.y = settings.Y_MIDDLE
        self.loot_x = pos[1]
        self.loot_y = pos[0]

    def throw_away_junk(self):
        pyautogui.moveTo(x=self.loot_x, y=self.loot_y)
        pyautogui.mouseDown()
        pyautogui.moveTo(self.x, self.y)
        pyautogui.mouseUp()

    def remove_junk_from_bp(self):
        while not detect('images/medicinepouch.png') == (self.y, self.x):
            if detect('images/fish.png'):
                for i in range(randint(1,3)):
                    pyautogui.press('f1')
            self.throw_away_junk()
        sleep(randint(20, 25))


