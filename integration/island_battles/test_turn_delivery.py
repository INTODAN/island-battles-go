"""A localhost bot gets each turn as a turn delivery and submits it itself.

The test plays the part of the player's game tab: it reads Alice's turn
delivery and posts it to `island-battles-bot serve`, which answers 202 and
submits the turn to the backend with the delivery's one-turn submit token.
The test never submits a command.
"""

import asyncio
import sys
import threading

import httpx
import pytest

from integration.test_live import BASE, create
from integration.test_live import guests as guests_fixture
from island_battles_client.cli import BotServer, ReloadingPlay
from lobby_client import Scope
from test_lobby_binding import choose_bot, game_auth

guests = guests_fixture
pytestmark = pytest.mark.asyncio

# Sends every soldier of each castle with more than one to the weakest castle
# it doesn't own. It raises on the turn after its first, which passes that turn.
BOT = """
from island_battles_client import send_soldier_group, turn_result


def play(turn):
    first = (turn.memory or {}).get("first", turn.generation.id)
    if turn.generation.id == first + 1:
        raise ValueError("no plan on the second turn")
    castles = turn.generation.castles
    targets = [castle for castle in castles if castle.team_color != turn.team_color]
    commands = []
    for castle in castles:
        if castle.team_color == turn.team_color and castle.soldiers > 1 and targets:
            target = min(targets, key=lambda other: other.soldiers)
            commands.append(send_soldier_group(castle.id, target.id, castle.soldiers - 1))
    return turn_result(commands, memory={"first": first})
"""


@pytest.fixture
def bot_server(tmp_path):
    (tmp_path / "delivered_bot.py").write_text(BOT)
    sys.path.insert(0, str(tmp_path))
    bot = ReloadingPlay("delivered_bot:play")
    bot.get()
    server = BotServer(("127.0.0.1", 0), bot, [], turn_timeout=5)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    yield server
    server.shutdown()
    server.server_close()
    sys.path.remove(str(tmp_path))
    sys.modules.pop("delivered_bot", None)


async def test_a_localhost_bot_plays_a_game_through_turn_deliveries(guests, bot_server):
    alice = await guests("Alice")
    room = await create(alice, "island-battles")
    scope = Scope.from_snapshot(room)
    red, blue = room.teams[0].id, room.teams[1].id
    await alice.add_seat(scope, str(blue), {"bot_level": 1})
    await alice.select_team(scope, str(red))
    await choose_bot(alice, scope.lobby_id, {"kind": "localhost"})
    await alice.set_ready(scope, True)
    await alice.start(scope)

    game = f"/game/from-lobby/{scope.lobby_id}"
    bot_url = f"http://127.0.0.1:{bot_server.server_address[1]}/turn"
    delivered = []
    async with httpx.AsyncClient(base_url=BASE, headers=await game_auth(alice), timeout=10) as http:
        deadline = asyncio.get_running_loop().time() + 600
        while True:
            assert asyncio.get_running_loop().time() < deadline, f"the game didn't end; delivered {delivered[-5:]}"
            read = await http.get(f"{game}/generation")
            assert read.status_code == 200, read.text
            state = read.json()
            # The outcome is absent while the game runs.
            if state.get("outcome") is not None:
                break
            if state["submitted"] or state["generation_id"] in delivered:
                await asyncio.sleep(0.05)
                continue
            response = await http.get(f"{game}/turn-delivery")
            if response.status_code == 409:
                # The bot completed the turn between the two reads.
                continue
            assert response.status_code == 200, response.text
            delivery = response.json()
            assert delivery["turn"]["generation"]["id"] == state["generation_id"]
            assert delivery["submit"]["url"] == f"{BASE}/generation/commands"
            assert delivery["submit"]["token"].startswith("ibst1.")
            # The bot answers at once and submits the turn itself.
            answer = await http.post(bot_url, json=delivery)
            assert (answer.status_code, answer.json()) == (202, {"accepted": True})
            delivered.append(state["generation_id"])

        assert len(delivered) >= 3, delivered
        assert state["outcome"]["reason"] != "turn_timeout", state["outcome"]
        # Every delivered turn was completed by the bot: no turn timed out,
        # and the turn after the first passed with the bot's error.
        logs = (await http.get(f"{game}/logs", params={"generation_id": delivered[1]})).json()
        [alice_logs] = [player for player in logs["players"] if player["display_name"] == "Alice"]
        [line] = alice_logs["lines"]
        assert (line["level"], line["logger"]) == ("error", "island_battles.bots")
        assert "so it passes this turn: ValueError: no plan on the second turn" in line["message"]
        # A submit token is refused for anything but its own turn.
        stale = (await http.get(f"{game}/turn-delivery"))
        assert stale.status_code == 409
        refused = await http.post(
            "/generation/commands",
            json={"game_id": state["game_id"], "generation_id": state["generation_id"], "request_id": "00000000-0000-4000-8000-000000000000", "commands": []},
            headers={"Authorization": "Bearer ibst1.e30.AAAA"},
        )
        assert refused.status_code == 401
