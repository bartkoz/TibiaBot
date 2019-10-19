import pyautogui


def get_life():
    # TODO: refactor
    health = pyautogui.locateOnScreen('images/health.png')
    if pyautogui.pixelMatchesColor((health.left + 105), (health.top + 7), (219, 79, 79)):
        return 100
    elif pyautogui.pixelMatchesColor((health.left + 94), (health.top + 7), (219, 79, 79)):
        return 90
    elif pyautogui.pixelMatchesColor((health.left + 74), (health.top + 7), (219, 79, 79)):
        return 70
    elif pyautogui.pixelMatchesColor((health.left + 54), (health.top + 7), (219, 79, 79)):
        return 50
    elif pyautogui.pixelMatchesColor((health.left + 34), (health.top + 7), (219, 79, 79)):
        return 30
    elif pyautogui.pixelMatchesColor((health.left + 24), (health.top + 7), (219, 79, 79)):
        return 20
    elif pyautogui.pixelMatchesColor((health.left + 14), (health.top + 7), (219, 79, 79)):
        return 15
    elif pyautogui.pixelMatchesColor((health.left + 5), (health.top + 7), (219, 79, 79)):
        return 10
    return 0
