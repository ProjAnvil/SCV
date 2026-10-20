from app.services import Greeter, make


def bootstrap():
    greeter = Greeter()
    return make(greeter)
