import numpy as np
from image_grab import detect, image_grab


class Health:

    def __init__(self):
        coordinates = detect('images/health.png')
        self.hp_x = coordinates[1]
        self.hp_y = coordinates[0] + 6
        self.rgb = (149, 73, 39)

    def _get_array(self):
        return np.array(image_grab())

    def get_life(self):
        array = self._get_array()
        if tuple(array[self.hp_y + 9][self.hp_x + 100][:3]) == self.rgb:
            return 100
        elif array[self.hp_y + 9][self.hp_x + 89] == self.rgb:
            return 90
        elif array[self.hp_y + 9][self.hp_x + 69] == self.rgb:
            return 70
        elif array[self.hp_y + 9][self.hp_x + 49] == self.rgb:
            return 50
        elif array[self.hp_y + 9][self.hp_x + 29] == self.rgb:
            return 30
        elif array[self.hp_y + 9][self.hp_x + 19] == self.rgb:
            return 20
        elif array[self.hp_y + 9][self.hp_x + 9] == self.rgb:
            return 15
        elif array[self.hp_y + 9][self.hp_x] == self.rgb:
            return 10
        return 0
