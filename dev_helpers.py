import settings


def dev_print(text):
    if settings.DEV_MODE:
        print('DEV NOTE: '.format(text))
