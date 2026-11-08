"""The host fills a team with a default bot seat and plays alone."""

import httpx
import pytest
from integration.test_live import BASE, create
from integration.test_live import guests as guests_fixture
from lobby_client import LobbyError, Scope
from test_bot_choice import run_bot
from test_lobby_binding import choose_bot

guests = guests_fixture
pytestmark = pytest.mark.asyncio


def seats(snapshot):
    return [member for member in snapshot.members if member.kind.value == "seat"]


async def test_one_person_plays_a_whole_game_against_a_level_two_bot_seat(guests):
    alice = await guests("Alice")
    room = await create(alice, "island-battles")
    scope = Scope.from_snapshot(room)
    red, blue = room.teams[0].id, room.teams[1].id

    # The game checks the seat's settings against its default bots.
    with pytest.raises(LobbyError) as invalid:
        await alice.add_seat(scope, str(blue), {"bot_level": 7})
    assert (invalid.value.code, invalid.value.status) == ("invalid_seat_settings", 422)
    assert "GET /bots/defaults" in invalid.value.detail

    seated = await alice.add_seat(scope, str(blue), {"bot_level": 2})
    [seat] = seats(seated)
    assert (seat.display_name, seat.seat_settings.to_dict(), seat.ready, str(seat.team_id)) == (
        "Bot (level 2)", {"bot_level": 2}, True, str(blue))
    await alice.select_team(scope, str(red))
    chosen = await choose_bot(alice, scope.lobby_id, {"kind": "localhost"})
    assert [(member["display_name"], member["bot"]) for member in chosen["members"]] == [
        ("Alice", {"kind": "localhost"}), ("Bot (level 2)", {"kind": "default", "level": 2})]
    await alice.set_ready(scope, True)
    started = await alice.start(scope)
    # The seat is on the frozen roster with its own player ID.
    assert {str(entry.player_id) for entry in started.start_roster} == {str(member.player_id) for member in started.members}
    assert str(seat.player_id) in {str(entry.player_id) for entry in started.start_roster}

    # Alice's localhost bot, run without a browser, plays the game to its end
    # against the backend's level 2 bot.
    status, output, error = await run_bot(alice, scope.lobby_id, 600)
    assert status == 0, error
    assert "game ended at generation" in output, output
    async with httpx.AsyncClient(base_url=BASE) as http:
        response = await http.get(
            f"/game/from-lobby/{scope.lobby_id}/generation",
            headers={"Authorization": f"Bearer {alice.credential}"},
        )
    assert response.status_code == 200, response.text
    state = response.json()
    assert state["bot"] == {"kind": "localhost"}
    assert state["outcome"] is not None

    # Once started, the seats are fixed.
    with pytest.raises(LobbyError) as late:
        await alice.remove_seat(scope, str(seat.id))
    assert late.value.code == "phase_conflict"


async def test_only_the_host_changes_seats_and_seats_alone_never_start(guests):
    alice, bob = await guests("Alice"), await guests("Bob")
    room = await create(alice, "island-battles")
    scope = Scope.from_snapshot(room)
    await bob.join_lobby(game_key=scope.game_key, join_code=room.lobby.join_code)
    with pytest.raises(LobbyError) as refused:
        await bob.add_seat(scope, str(room.teams[1].id), {"bot_level": 1})
    assert (refused.value.code, refused.value.status) == ("not_host", 403)

    first = seats(await alice.add_seat(scope, str(room.teams[0].id), {"bot_level": 1}))[0]
    both = seats(await alice.add_seat(scope, str(room.teams[1].id), {"bot_level": 3}))
    assert [seat.display_name for seat in both] == ["Bot (level 1)", "Bot (level 3)"]
    with pytest.raises(LobbyError) as refused:
        await bob.remove_seat(scope, str(first.id))
    assert refused.value.code == "not_host"
    assert len(seats(await alice.remove_seat(scope, str(first.id)))) == 1

    # Two bot seats and no person: once the people leave, nobody hosts the
    # lobby, and it closes rather than start.
    await alice.add_seat(scope, str(room.teams[0].id), {"bot_level": 1})
    await bob.leave(scope)
    with pytest.raises(LobbyError) as not_ready:
        await alice.start(scope)
    assert not_ready.value.code == "not_ready"
    await alice.leave(scope)
    async with httpx.AsyncClient(base_url=BASE) as http:
        response = await http.post(
            "/lobby/v1/memberships",
            json={"game_key": scope.game_key, "join_code": room.lobby.join_code, "client_instance_id": str(bob.client_instance_id)},
            headers={"Authorization": f"Bearer {bob.credential}"},
        )
    assert (response.status_code, response.json()["code"]) == (404, "not_found")


async def test_a_quiz_demo_lobby_refuses_seats(guests):
    alice = await guests("Alice")
    room = await create(alice, "quiz-demo")
    scope = Scope.from_snapshot(room)
    with pytest.raises(LobbyError) as refused:
        await alice.add_seat(scope, str(room.teams[1].id), {"bot_level": 2})
    assert (refused.value.code, refused.value.status) == ("invalid_seat_settings", 422)
    assert all(member.kind.value == "human" for member in (await alice.snapshot(scope)).members)
