import pyautogui


def get_mana():
    # TODO: refactor
    health = pyautogui.locateOnScreen('images/mana.png')
    try:
        if pyautogui.pixelMatchesColor((health.left + 105), (health.top + 5), (67, 64, 192)):
            return 100
        elif pyautogui.pixelMatchesColor((health.left + 94), (health.top + 5), (67, 64, 192)):
            return 90
        elif pyautogui.pixelMatchesColor((health.left + 74), (health.top + 5), (67, 64, 192)):
            return 70
        elif pyautogui.pixelMatchesColor((health.left + 54), (health.top + 5), (67, 64, 192)):
            return 50
        elif pyautogui.pixelMatchesColor((health.left + 34), (health.top + 7), (219, 79, 79)):
            return 30
        elif pyautogui.pixelMatchesColor((health.left + 24), (health.top + 7), (219, 79, 79)):
            return 20
        elif pyautogui.pixelMatchesColor((health.left + 14), (health.top + 7), (219, 79, 79)):
            return 15
        elif pyautogui.pixelMatchesColor((health.left + 5), (health.top + 28), (219, 79, 79)):
            return 10
        return 0
    except AttributeError:
        print("An error occurred while trying to read mana")