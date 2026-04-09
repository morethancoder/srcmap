class HttpClient:
    def __init__(self, base_url: str):
        self.base_url = base_url

    def get(self, path: str, params: dict = None) -> dict:
        pass

    def post(self, path: str, data: dict = None) -> dict:
        pass

def create_session(timeout: int = 30) -> HttpClient:
    return HttpClient("http://localhost")

class Response:
    def json(self) -> dict:
        pass

    def text(self) -> str:
        pass
