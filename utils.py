import settings
import pyautogui
from image_grab import detect
from random import randint
from time import sleep


class JunkRemover:

    def __init__(self):
        self.pos = detect('images/medicinepouch.png')
        self.x = settings.X_MIDDLE
        self.y = settings.Y_MIDDLE
        self.loot_x = self.pos[0]
        self.loot_y = self.pos[1]

    def throw_away_junk(self):
        pyautogui.moveTo(x=self.loot_y, y=self.loot_x)
        pyautogui.mouseDown()
        pyautogui.moveTo(self.x, self.y)
        pyautogui.mouseUp()

    def remove_junk_from_bp(self):
        # while True:
        print('Checking if anything to throw away...')
        if detect('images/medicinepouch.png') != (self.loot_x, self.loot_y):
            if detect('images/fish.png'):
                for _ in range(randint(2, 5)):
                    sleep(0.3)
                    pyautogui.keyDown('F1')
            pyautogui.keyDown('escape')
            self.throw_away_junk()
            # sleep(randint(5, 7))
