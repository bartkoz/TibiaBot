import pyautogui
from time import sleep

from health import Health
from mana import Mana


if __name__ == "__main__":
    health_obj = Health()
    mana_obj = Mana()
    while True:
        if health_obj.get_life() <= 80:
            pyautogui.keyDown('f2')
            sleep(1)
        if mana_obj.get_mana() == 90:
            pyautogui.keyDown('f2')
            sleep(1)