import pyautogui
import settings
from random import randint
from image_grab import detect


xMiddle = settings.xMiddle
yMiddle = settings.yMiddle


class Attack:

    def __init__(self):
        self.attacking = False

    def detect_enemy(self):
        """
        Detects if enemy is visible on screen
        """
        for monster_name in settings.monsters_names:
            while not self.attacking:
                if detect('images/{}.png'.format(monster_name)):
                    print('monster found, attacking')
                    self.perform_attack(monster_name)

    def perform_attack(self, monster_name):
        """
        Attacks with space
        """
        if not self.check_if_attacking(monster_name):
            pyautogui.keyDown('space')
            self.attacking = True

    def check_if_attacking(self, monster_name):
        if detect('images/{}_attacking.png'.format(monster_name)):
            self.attacking = True
            return True
        self.attacking = False
        return False


def collect_loot():
    """
    Collects loot on all 8 sqms around character, to be changed, in the future
    """
    offset = 40 + randint(10, 15)
    pos_list = [
        [xMiddle - offset, yMiddle],
        [xMiddle - offset, yMiddle + offset],
        [xMiddle, yMiddle + offset],
        [xMiddle + offset, yMiddle + offset],
        [xMiddle + offset, yMiddle],
        [xMiddle + offset, yMiddle - offset],
        [xMiddle, yMiddle - offset],
        [xMiddle - offset, yMiddle - offset],
    ]
    pyautogui.PAUSE = 0.00001
    pyautogui.keyDown('shift')
    for position in pos_list:
        pyautogui.click(x=position[0], y=position[1], button='right')
        pyautogui.keyUp('shift')
