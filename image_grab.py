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
        monitor = {"top": 0, "left": 0, "width": settings.SCREEN_X_RESOLUTION, "height": settings.SCREEN_Y_RESOLUTION}
        return sct.grab(monitor)


def detect(image, threshold=0.99):
    """
    :param image: file name with location eg. images/test.png
    :param threshold: how accurate template matching should be
    :return: x,y coordinates in (x,y) tuple, returns False if
    nothing has been recognized
    """
    img = np.array(image_grab())
    template = cv2.imread('{}'.format(image), cv2.IMREAD_UNCHANGED)
    res = cv2.matchTemplate(img, template, cv2.TM_CCORR_NORMED)
    x, y = np.where(res >= threshold)
    try:
        return x[0], y[0]
    except IndexError:
        return False
