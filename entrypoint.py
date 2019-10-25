from cmd import Cmd


class TibiaBot(Cmd):
    prompt = 'TibiaBot> '
    intro = "Welcome to TibiaBot! Type ? to list commands or simply type 'configure' to start configuration module."

    def do_exit(self, inp):
        print("Closing...")
        return True

    def do_configure(self, inp):
        x_middle = input('X coordinate of middle of your screen (middle of character: ')
        y_middle = input('Y coordinate of middle of your screen (middle of character: ')
        monsters = input('Input monsters names (keep in mind these have to reflect files names for image detection'
                         'in such manner: ["rat", "cave_rat"]: ')
        while not isinstance(monsters, list):
            monsters = input('Your monsters list seems to be corupted, please try again: ')
        number_of_wpts = input('Number of waypoints used in cavebot module: ')
        screen_x_res = input('Your screen resolution (X): ')
        screen_y_res = input('Your screen resolution (Y): ')
        offset = input('how many pixels does the "square" occupy, used for looting: ')

    def help_configure(self):
        print("Runs bot configuration, make sure all data is put correctly.")


if __name__ == '__main__':
    TibiaBot().cmdloop()