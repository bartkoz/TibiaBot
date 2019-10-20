import settings


def dev_print(text):
    if settings.DEV_MODE:
        print(text)
