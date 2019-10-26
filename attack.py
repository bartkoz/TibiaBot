import pyautogui
import cv2

import settings
from random import randint
from image_grab import detect
from dev_helpers import dev_print
from utils import Array

xMiddle = settings.X_MIDDLE
yMiddle = settings.Y_MIDDLE


class Attack(Array):

    def __init__(self):
        self.loot_collected = True
        self.follow_coordinates = detect('images/follow.png')
        self.battle_coordinates = detect('images/battle.png')
        self.no_monster_on_screen_rgb = (70, 70, 70)

    def attack(self):
        """
        Main method gathering compiling all other defs.
        :return:
        """
        dev_print('self.loot_collected set to: {}'.format(self.loot_collected))
        for monster_name in settings.MONSTER_NAMES:
            self.perform_loot_collection(monster_name)
            if self.detect_enemy(monster_name) and not self.check_if_attacking(monster_name):
                print('performing attack procedure')
                self.perform_loot_collection(monster_name)
                self.perform_attack(monster_name)

    def perform_attack(self, monster_name):
        """
        Attacks with space
        """
        print('Marked {}.'.format(monster_name))
        pyautogui.click(x=self.follow_coordinates[1], y=self.follow_coordinates[0])
        pyautogui.keyDown('space')
        self.loot_collected = False

    def perform_loot_collection(self, monster_name):
        if not self.check_if_attacking(monster_name) and not self.loot_collected:
            self.collect_loot()

    def check_if_attacking(self, monster_name):
        if detect('images/{}_attacking.png'.format(monster_name), threshold=0.98):
            return True
        return False

    def detect_enemy(self, monster_name):
        """
        Detects whether enemy is available to attack
        :param monster_name: name of monster, for template matching
        :return: Bool
        """
        array = self._get_array()
        array = cv2.cvtColor(array, cv2.COLOR_BGR2RGB)
        if tuple(array[self.battle_coordinates[0] + 20][self.battle_coordinates[1] + 6][
                 :3]) != self.no_monster_on_screen_rgb:  # noqa
            return True
        return False

    def collect_loot(self):
        """
        Collects loot on all 8 sqms around character, to be changed, in the future
        """
        print('Collecting loot...')
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
