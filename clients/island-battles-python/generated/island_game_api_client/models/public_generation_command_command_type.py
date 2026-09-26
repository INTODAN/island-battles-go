from enum import StrEnum


class PublicGenerationCommandCommandType(StrEnum):
    SEND_SOLDIER_GROUP = "send_soldier_group"

    def __str__(self) -> str:
        return str(self.value)
