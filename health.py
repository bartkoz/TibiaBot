import cv2

from image_grab import detect
from utils import Array


class Health(Array):

    # TODO: refactor
    def __init__(self):
        coordinates = detect('images/health.png')
        self.hp_x = coordinates[1] + 5
        self.hp_y = coordinates[0] + 7
        self.rgb = (255, 113, 113)

    def get_life(self):
        while True:
            array = self._get_array()
            array = cv2.cvtColor(array, cv2.COLOR_BGR2RGB)
            if tuple(array[self.hp_y][self.hp_x + 90][:3]) == self.rgb:
                return 90
            elif tuple(array[self.hp_y][self.hp_x + 81][:3]) == self.rgb:
                return 80
            elif tuple(array[self.hp_y][self.hp_x + 72][:3]) == self.rgb:
                return 70
            elif tuple(array[self.hp_y][self.hp_x + 63][:3]) == self.rgb:
                return 60
            elif tuple(array[self.hp_y][self.hp_x + 54][:3]) == self.rgb:
                return 50
            elif tuple(array[self.hp_y][self.hp_x + 45][:3]) == self.rgb:
                return 40
            elif tuple(array[self.hp_y][self.hp_x + 36][:3]) == self.rgb:
                return 30
            elif tuple(array[self.hp_y][self.hp_x + 27][:3]) == self.rgb:
                return 20
            elif tuple(array[self.hp_y][self.hp_x + 18][:3]) == self.rgb:
                return 10
            return 0
