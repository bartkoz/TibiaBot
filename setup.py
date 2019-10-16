import json
from pyautogui import *


def configurate():
    return {
        'constant_locator_distance': calculate_distance_from_const_to_waypoint(),
        'constant_locator_location': locate_constant_locator()
    }


def read_config():
    try:
        with open('config.json', 'r') as f:
            print('Config found, processing with current settings.')
            return json.load(f)
    except IOError:
        print('Config not found, processing with configuration')
        with open('config.json', 'w') as f:
            cfg = configurate()
            f.write(json.dumps(cfg))
            return cfg


def locate_constant_locator():
    return locateCenterOnScreen('images/constant_locator.png').x


def calculate_distance_from_const_to_waypoint():
    try:
        return locate_constant_locator().x - locateCenterOnScreen('waypoints/1.png').x
    except TypeError:
        print('Could not find loupe or waypoint on screen, are you sure you have Tibia client focused?')