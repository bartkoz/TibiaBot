import settings
import pyautogui
from image_grab import detect

xMiddle = settings.xMiddle
yMiddle = settings.yMiddle


def move_mouse_to_center():
    moveTo(xMiddle, yMiddle)


def perform_movement(config):
    locator_x_distance = config['constant_locator_x_distance']
    locator_y_distance = config['constant_locator_y_distance']
    locator_x_position = config['constant_locator_x_location']
    locator_y_position = config['constant_locator_y_location']

    def waypoint_achieved(waypoint_number):
        x_locator = locator_x_position[1] - detect('waypoints/{}.png'.format(waypoint_number))[1] == locator_x_distance
        y_locator = locator_y_position[0] - detect('waypoints/{}.png'.format(waypoint_number))[0] == locator_y_distance
        if x_locator and y_locator:
            return True
        return False

    def move_to_waypoint(waypoint_number):
        if not waypoint_number:
            raise ValueError('Waypoint number has not been provided')
        coordinates = detect('waypoints/{}.png'.format(waypoint_number))
        pyautogui.click(coordinates[1], coordinates[0])

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
