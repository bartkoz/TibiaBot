import pyautogui as pau
from pyautogui import *
from random import randint
import settings
from setup import read_config

xMiddle = settings.xMiddle
yMiddle = settings.yMiddle


def move_mouse_to_center():
    moveTo(xMiddle, yMiddle)


def perform_movement():
    config = read_config()
    locator_distance = config['constant_locator_distance']
    locator_position = config['constant_locator_location']

    def waypoint_achieved(waypoint_number):
        if locator_position[0] - locateCenterOnScreen('waypoints/{}.png'.format(waypoint_number)).x != locator_distance:
            return False
        return True

    def move_to_waypoint(waypoint_number):
        if not waypoint_number:
            raise ValueError('Waypoint number has not been provided')
        click(locateCenterOnScreen('waypoints/{}.png'.format(waypoint_number)))

    for waypoint_number in range(1, settings.number_of_wpts+1):
        print('Moving to {}'.format(waypoint_number))
        print('performing movement')
        move_to_waypoint(waypoint_number)
        while not waypoint_achieved(waypoint_number):
            print('waypoint not achieved')
            move_to_waypoint(waypoint_number)
        print('waypoint achieved')


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
