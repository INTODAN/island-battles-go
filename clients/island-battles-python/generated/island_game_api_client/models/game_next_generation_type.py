from enum import StrEnum


class GameNextGenerationType(StrEnum):
    GAME_NEXT_GENERATION = "game.next_generation"

    def __str__(self) -> str:
        return str(self.value)
