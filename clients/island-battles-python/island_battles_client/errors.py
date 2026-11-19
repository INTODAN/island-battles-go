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


class GameCommandError(GameError):
    """A command that the game rules reject, for a reason a strategy can act on.

    The server rejects the whole result: its commands are not stored and the
    turn stays open. ``status`` is the HTTP status.
    """


class CastleNotFoundError(GameCommandError):
    """The castle is not in the generation."""


class CastleNotOwnedError(GameCommandError):
    """The castle belongs to another team or to none."""


class UpgradeMaxLevelError(GameCommandError):
    """The upgrade type is at its maximum level, counting this turn's upgrades."""


class InsufficientSoldiersError(GameCommandError):
    """The castle has too few soldiers left after this turn's reservations."""


class BurstUnavailableError(GameCommandError):
    """The castle has burst debt or a burst this turn."""


class BurstAbovePotentialError(GameCommandError):
    """The burst is above the castle's burst potential, counting this turn's upgrades."""


class GameLogLimitError(GameError):
    """The server refused a batch's game log lines, which would pass its limit.

    The client handles it itself: it sends the batch's commands again without
    the lines, so a strategy never sees it.
    """


class BotMemoryTooLargeError(GameError):
    """A bot's memory is over 64 KiB as compact JSON.

    The client raises it before any request, with ``status`` None. The server
    refuses such a batch with the same error and its HTTP status.
    """


class InvalidBotResultError(GameError):
    """A bot returned something other than a valid ``BotTurnResult``."""


class GameEndedError(GameError):
    """The server refused a batch because the game has ended.

    A turn that passes its deadline ends the game while a slow callback may
    still be sending commands. The client handles it itself: it reads the
    final state and calls ``on_game_ended``, so the client doesn't fail.
    """


class StaleGenerationError(GameError):
    """The server refused a batch because its generation's turn is over.

    In a real-time game the turn ends one turn pace after its generation was
    created, and the server plays it with the commands that arrived by then,
    so a bot slower than the pace passes that turn. The client handles it
    itself there: it writes one line, reads the new state and plays on. In a
    game that waits for every player the client stops on it, as on any
    other rejection.
    """


# The server's problem types that clients tell apart: the command rejections,
# shared by every command, the game's end, a stale generation, the game log
# limit and the bot memory limit.
PROBLEM_TYPE_ERRORS: dict[str, type[GameError]] = {
    "urn:island-battles:problem:game-ended": GameEndedError,
    "urn:island-battles:problem:stale-generation": StaleGenerationError,
    "urn:island-battles:problem:castle-not-found": CastleNotFoundError,
    "urn:island-battles:problem:castle-not-owned": CastleNotOwnedError,
    "urn:island-battles:problem:upgrade-max-level": UpgradeMaxLevelError,
    "urn:island-battles:problem:insufficient-soldiers": InsufficientSoldiersError,
    "urn:island-battles:problem:burst-unavailable": BurstUnavailableError,
    "urn:island-battles:problem:burst-above-potential": BurstAbovePotentialError,
    "urn:island-battles:problem:game-log-limit": GameLogLimitError,
    "urn:island-battles:problem:bot-memory-too-large": BotMemoryTooLargeError,
}
