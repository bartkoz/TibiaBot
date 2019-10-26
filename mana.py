import cv2
from image_grab import detect
from utils import Array


class Mana(Array):

    # TODO: refactor
    def __init__(self):
        coordinates = detect('images/mana.png')
        self.mana_x = coordinates[1] + 5
        self.mana_y = coordinates[0] + 6
        self.rgb = (101, 98, 240)

    def get_mana(self):
        array = self._get_array()
        array = cv2.cvtColor(array, cv2.COLOR_BGR2RGB)
        if tuple(array[self.mana_y][self.mana_x + 90][:3]) == self.rgb:
            return 90
        elif tuple(array[self.mana_y][self.mana_x + 81][:3]) == self.rgb:
            return 80
        elif tuple(array[self.mana_y][self.mana_x + 72][:3]) == self.rgb:
            return 70
        elif tuple(array[self.mana_y][self.mana_x + 63][:3]) == self.rgb:
            return 60
        elif tuple(array[self.mana_y][self.mana_x + 54][:3]) == self.rgb:
            return 50
        elif tuple(array[self.mana_y][self.mana_x + 45][:3]) == self.rgb:
            return 40
        elif tuple(array[self.mana_y][self.mana_x + 36][:3]) == self.rgb:
            return 30
        elif tuple(array[self.mana_y][self.mana_x + 27][:3]) == self.rgb:
            return 20
        elif tuple(array[self.mana_y][self.mana_x + 18][:3]) == self.rgb:
            return 10
        return 0
