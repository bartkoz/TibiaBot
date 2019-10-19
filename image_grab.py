import mss.tools
import cv2
import numpy as np


def image_grab():
    """
    Helper method for efficient screen capture without saving image anywhere
    :return: ScreenShot object, used in detect()
    """
    with mss.mss() as sct:
        monitor = {"top": 0, "left": 0, "width": 2560, "height": 1440}
        return sct.grab(monitor)


def detect(image):
    """
    :param image: file name with location eg. images/test.png
    :return: x,y coordinates in nested tuple [0][0] [0][1], can be empty
    if nothing has been detected
    """
    img = cv2.cvtColor(np.array(image_grab()), cv2.COLOR_BGR2GRAY)
    template = cv2.imread('{}'.format(image), 0)
    res = cv2.matchTemplate(img, template, cv2.TM_CCORR_NORMED)
    threshold = 0.99
    x, y = np.where(res >= threshold)
    try:
        return x[0], y[0]
    except IndexError:
        return False
