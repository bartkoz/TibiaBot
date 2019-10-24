import json
from image_grab import detect


def configurate():
    return {
        'constant_locator_x_distance': calculate_distance_from_const_to_waypoint()[1],
        'constant_locator_y_distance': calculate_distance_from_const_to_waypoint()[0],
        'constant_locator_x_location': locate_constant_locator('constant_locator'),
        'constant_locator_y_location': locate_constant_locator('constant_locator2')

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


def locate_constant_locator(locator):
    return detect('images/{}.png'.format(locator))


def calculate_distance_from_const_to_waypoint():
    try:
        x = locate_constant_locator('constant_locator')[1] - detect('waypoints/1.png')[1]
        y = locate_constant_locator('constant_locator2')[0] - detect('waypoints/1.png')[0]
        return x, y
    except TypeError:
        print('Could not find loupe or waypoint on screen, are you sure you have Tibia client focused?')
