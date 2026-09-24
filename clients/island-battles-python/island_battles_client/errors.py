"""Public failures with safe HTTP status and detail, without credentials or bodies."""


class GameError(Exception):
    def __init__(self, detail: str, status: int | None = None):
        self.detail = detail
        self.status = status
        super().__init__(f"HTTP {status}: {detail}" if status is not None else detail)


class GameAuthenticationError(GameError):
    pass


class GameTransportError(GameError):
    pass


class GameProtocolError(GameError):
    pass


class GameClientClosedError(GameError):
    def __init__(self):
        super().__init__("client closed")
