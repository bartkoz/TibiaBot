from pyautogui import *


def detect_enemy():
    for monster_name in settings.monsters_names:
        if locateOnScreen('images/{}.png'.format(monster_name)):
            perform_attack(monster_name)


def perform_attack(monster_name):
    while not locateOnScreen('images/attacking.png'):
        click(locateCenterOnScreen('images/follow.png'))
        click(locateCenterOnScreen('images/{}.png'.format(monster_name)))


def collect_loot():
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
    pau.PAUSE = 0.00001
    keyDown('shift')
    for position in pos_list:
        click(x=position[0], y=position[1], button='right')
    keyUp('shift')
