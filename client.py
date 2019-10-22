import multiprocessing
from walk import Walk
from utils import JunkRemover


if __name__ == "__main__":
    p1 = multiprocessing.Process(target=Walk().perform_movement())
    p2 = multiprocessing.Process(target=JunkRemover.remove_junk_from_bp())

    p1.start()
    p2.start()

    p1.join()
    p2.join()
