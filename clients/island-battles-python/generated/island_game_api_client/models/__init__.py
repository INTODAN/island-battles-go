"""Contains all the data models used in inputs/outputs"""

from .castle_view import CastleView
from .clash_event import ClashEvent
from .clash_participant import ClashParticipant
from .connection_token import ConnectionToken
from .connection_token_input_body import ConnectionTokenInputBody
from .error_detail import ErrorDetail
from .error_model import ErrorModel
from .game_next_generation import GameNextGeneration
from .game_next_generation_schema_version import GameNextGenerationSchemaVersion
from .game_next_generation_type import GameNextGenerationType
from .generation_state import GenerationState
from .generation_view import GenerationView
from .problem import Problem
from .problem_code import ProblemCode
from .public_generation_command import PublicGenerationCommand
from .public_generation_command_command_type import PublicGenerationCommandCommandType
from .register_generation_commands import RegisterGenerationCommands
from .send_soldier_group_parameters import SendSoldierGroupParameters
from .soldier_group_view import SoldierGroupView

__all__ = (
    "CastleView",
    "ClashEvent",
    "ClashParticipant",
    "ConnectionToken",
    "ConnectionTokenInputBody",
    "ErrorDetail",
    "ErrorModel",
    "GameNextGeneration",
    "GameNextGenerationSchemaVersion",
    "GameNextGenerationType",
    "GenerationState",
    "GenerationView",
    "Problem",
    "ProblemCode",
    "PublicGenerationCommand",
    "PublicGenerationCommandCommandType",
    "RegisterGenerationCommands",
    "SendSoldierGroupParameters",
    "SoldierGroupView",
)
