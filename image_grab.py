import settings
import mss.tools
import cv2
import numpy as np


def image_grab():
    """
    Helper method for efficient screen capture without saving image anywhere
    :return: ScreenShot object, used in detect()
    """
    with mss.mss() as sct:
        monitor = {"top": 0, "left": 0, "width": settings.screenResolutionX, "height": settings.screenResolutionY}
        return sct.grab(monitor)


def detect(image):
    """
    :param image: file name with location eg. images/test.png
    :return: x,y coordinates in (x,y) tuple, returns False if
    nothing has been recognized
    """
    img = np.array(image_grab())
    template = cv2.imread('{}'.format(image), cv2.IMREAD_UNCHANGED)
    res = cv2.matchTemplate(img, template, cv2.TM_CCORR_NORMED)
    threshold = 0.99
    x, y = np.where(res >= threshold)
    try:
        return x[0], y[0]
    except IndexError:
        return False
