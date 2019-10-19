import pyautogui
import settings
from random import randint
from image_grab import detect


xMiddle = settings.xMiddle
yMiddle = settings.yMiddle


def detect_enemy():
    """
    Detects if enemy is visible on screen
    """
    for monster_name in settings.monsters_names:
        if detect('images/{}.png'.format(monster_name)):
            print('monster found, attacking')
            perform_attack()


def perform_attack():
    """
    Changes stance to 'follow' and attacks with space
    """
    x, y = detect('images/follow.png')
    pyautogui.click(x=x, y=y)
    pyautogui.keyDown('space')


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
