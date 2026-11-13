"""A lobby plays a game to its end, goes back to waiting and plays a second game."""
import asyncio

import httpx
import pytest
from lobby_client import LobbyError, Scope

from integration.test_live import create, game_sql, guests, latest
from test_lobby_binding import GAME, LOCALHOST_BOT, choose_bot, game_auth

pytestmark = pytest.mark.asyncio

# The shortest turn the game allows, so a turn that nobody completes ends the
# game soon.
TURN_TIMEOUT_SECONDS = 5


async def play_until_timeout(http, player, lobby_id):
    """Nobody completes the turn: once its deadline has passed, applying it
    ends the game with turn_timeout. Returns the ended state."""
    headers = await game_auth(player)
    async with asyncio.timeout(TURN_TIMEOUT_SECONDS + 20):
        while True:
            response = await http.post(f"/game/from-lobby/{lobby_id}/deadline", headers=headers)
            assert response.status_code == 200, response.text
            state = response.json()
            if state.get("outcome"):
                return state
            await asyncio.sleep(1)


async def test_a_lobby_plays_a_second_game_after_the_first_ends(guests):
    alice, bob, carol = await guests("Alice"), await guests("Bob"), await guests("Carol")
    room = await create(alice, "island-battles", {"turn_timeout_seconds": TURN_TIMEOUT_SECONDS})
    scope = Scope.from_snapshot(room)
    assert room.lobby.round_ == 1
    await bob.join_lobby(game_key=scope.game_key, join_code=room.lobby.join_code)
    for player, team in ((alice, room.teams[0]), (bob, room.teams[1])):
        await player.select_team(scope, team.id)
        await choose_bot(player, scope.lobby_id, LOCALHOST_BOT)
        await player.set_ready(scope, True)
    first = await alice.start(scope)
    assert (first.lobby.phase, first.lobby.round_) == ("started", 1)

    # While the game runs, the lobby can't be reopened.
    with pytest.raises(LobbyError) as running:
        await bob.reopen(scope)
    assert (running.value.code, running.value.status) == ("game_running", 409)

    async with httpx.AsyncClient(base_url=GAME) as http:
        ended = await play_until_timeout(http, alice, scope.lobby_id)
        assert ended["outcome"]["reason"] == "turn_timeout" and ended["lobby_round"] == 1
        first_game = ended["game_id"]

        # Bob goes back to the lobby: it waits in round 2 with the same
        # players, teams and bots, and Alice sees it pushed. Alice's own Back
        # to lobby a moment later changes nothing.
        reopened = await bob.reopen(scope)
        assert (reopened.lobby.phase, reopened.lobby.round_, reopened.start_roster) == ("waiting", 2, [])
        assert [str(team.id) for team in reopened.teams] == [str(team.id) for team in room.teams]
        seen = await latest(alice, scope, reopened.lobby.version)
        assert seen.lobby.round_ == 2
        assert [(member.display_name, str(member.team_id), member.ready) for member in seen.members] == [
            ("Alice", str(room.teams[0].id), False), ("Bob", str(room.teams[1].id), False)]
        again = await alice.reopen(scope)
        assert (again.lobby.version, again.lobby.round_) == (reopened.lobby.version, 2)

        # The lobby waits as before a first game: Bob leaves, Carol joins and
        # takes his team, and the host starts the second game.
        await bob.leave(scope)
        await carol.join_lobby(game_key=scope.game_key, join_code=room.lobby.join_code)
        await carol.select_team(scope, room.teams[1].id)
        await choose_bot(carol, scope.lobby_id, LOCALHOST_BOT)
        for player in (alice, carol):
            await player.set_ready(scope, True)
        second = await alice.start(scope)
        assert (second.lobby.phase, second.lobby.round_, len(second.start_roster)) == ("started", 2, 2)

        async def generation(player):
            return await http.get(f"/game/from-lobby/{scope.lobby_id}/generation",
                                  headers=await game_auth(player))

        state = (await generation(carol)).json()
        assert state["lobby_round"] == 2 and state["game_id"] != first_game and state.get("outcome") is None
        # Bob played the first game, not the second: the lobby's game is now
        # the second, and he isn't on its roster.
        assert (await generation(bob)).status_code == 404
        replay = await http.get(f"/game/from-lobby/{scope.lobby_id}/replay?lobby_round=1",
                                headers=await game_auth(alice))
        assert replay.status_code == 404
        assert await game_sql(f"SELECT string_agg(lobby_round::text, ',' ORDER BY lobby_round) FROM games WHERE lobby_id='{scope.lobby_id}'") == "1,2"

        # The second game plays to its end too, and the lobby can be reopened again.
        ended = await play_until_timeout(http, carol, scope.lobby_id)
        assert ended["lobby_round"] == 2 and ended["outcome"]["reason"] == "turn_timeout"
        third = await carol.reopen(scope)
        assert (third.lobby.phase, third.lobby.round_) == ("waiting", 3)
