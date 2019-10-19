import settings
from pyautogui import *
from image_grab import detect

xMiddle = settings.xMiddle
yMiddle = settings.yMiddle


def move_mouse_to_center():
    moveTo(xMiddle, yMiddle)


def perform_movement(config):
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
        try:
            print('Moving to {}'.format(waypoint_number))
            print('performing movement')
            move_to_waypoint(waypoint_number)
            while not waypoint_achieved(waypoint_number):
                print('waypoint not achieved')
                move_to_waypoint(waypoint_number)
            print('waypoint achieved')
        except TypeError:
            print('An error occurred, moving to next waypoint')
            pass
