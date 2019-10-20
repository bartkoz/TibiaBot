import pyautogui
import settings
from random import randint
from image_grab import detect
from dev_helpers import dev_print


xMiddle = settings.X_MIDDLE
yMiddle = settings.Y_MIDDLE


class Attack:

    def __init__(self):
        self.attacking = False
        self.loot_collected = False

    def attack(self):
        """
        Main method gathering compiling all other defs.
        :return:
        """
        while True:
            dev_print('self.attacking Bool set to: {}'.format(self.attacking))
            for monster_name in settings.MONSTER_NAMES:
                if self.detect_enemy(monster_name) and not self.check_if_attacking(monster_name):
                    print('performing attack procedure')
                    self.perform_attack(monster_name)
            if not self.attacking and not self.loot_collected:
                self.collect_loot()

    def perform_attack(self, monster_name):
        """
        Attacks with space
        """
        print('Marked {}.'.format(monster_name))
        pyautogui.keyDown('space')

    def check_if_attacking(self, monster_name):
        for image in [monster_name, '{}2'.format(monster_name)]:
            if detect('images/{}_attacking.png'.format(image), threshold=0.98):
                self.attacking = True
                return True
            self.attacking = False
            return False

    def detect_enemy(self, monster_name):
        """
        Detects whether enemy is available to attack
        :param monster_name: name of monster, for template matching
        :return: Bool
        """
        if detect('images/{}.png'.format(monster_name)):
            return True
        return False

    def collect_loot(self):
        """
        Collects loot on all 8 sqms around character, to be changed, in the future
        """
        offset = settings.OFFSET + randint(10, 15)
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
        self.loot_collected = True
