"""IslandBattlesClient plays only a localhost bot's seat; it says which bot the
server plays any other seat with."""

import pytest

from island_battles_client._generated import BotChoice
from island_battles_client.client import played_by_server


@pytest.mark.parametrize(
    ("bot", "says"),
    [
        ({"kind": "default", "level": 2}, "its level 2 default bot"),
        (
            {"kind": "hosted", "host": "bot.example.com"},
            "your hosted bot at bot.example.com",
        ),
        ({"kind": "uploaded"}, "your uploaded bot"),
    ],
)
def test_the_client_names_the_bot_the_server_plays(
    bot: dict[str, object], says: str
) -> None:
    message = played_by_server(BotChoice.model_validate(bot))
    assert says in message
    assert message.endswith(
        "choose your localhost bot in the lobby before getting ready"
    )
