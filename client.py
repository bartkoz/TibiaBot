from setup import read_config
from walk import perform_movement
from mana import get_mana
from multiprocessing import Process


class Bot:

    def __init__(self):
        self.config = read_config()
        self.performing_action = False

    def move(self):
        perform_movement(self.config)

    def get_mana_status(self):
        print(get_mana())

    def run(self):
        while True:
            Process(target=self.get_mana_status).start()
            # Process(target=self.move).start()


bot = Bot()
bot.run()
