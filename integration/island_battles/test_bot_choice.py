"""Every Island Battles player chooses a bot before getting ready: their own
localhost bot, whose turns their client relays, or a default level that the
backend plays."""

import asyncio
import sys
from pathlib import Path

import httpx
import pytest
from integration.test_live import BASE, create
from integration.test_live import guests as guests_fixture
from lobby_client import LobbyError, Scope
from test_lobby_binding import choose_bot, game_auth, member_settings_path

guests = guests_fixture
pytestmark = pytest.mark.asyncio

PYTHON_CLIENT = Path(__file__).resolve().parents[2] / "clients/island-battles-python"


async def run_bot(player, lobby_id, timeout):
    """Play a seat with ``island-battles-bot run`` and the example bot."""
    process = await asyncio.create_subprocess_exec(
        sys.executable, "-m", "island_battles_client.cli", "run", "examples.bot:play",
        "--base-url", BASE, f"--token={player.credential}", "--lobby-id", lobby_id,
        cwd=PYTHON_CLIENT, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
    )
    try:
        output, error = await asyncio.wait_for(process.communicate(), timeout)
    except TimeoutError:
        process.kill()
        await process.wait()
        raise
    return process.returncode, output.decode(), error.decode()


async def test_a_localhost_bot_plays_a_default_level_to_the_end(guests):
    alice, bob = await guests("Alice"), await guests("Bob")
    room = await create(alice, "island-battles")
    scope = Scope.from_snapshot(room)
    await bob.join_lobby(game_key=scope.game_key, join_code=room.lobby.join_code)
    await alice.select_team(scope, room.teams[0].id)
    await bob.select_team(scope, room.teams[1].id)
    with pytest.raises(LobbyError) as refused:
        await alice.set_ready(scope, True)
    assert (refused.value.code, refused.value.status, refused.value.problem_type) == (
        "game_requirement", 409, "urn:island-battles:problem:bot-not-chosen")
    assert "Choose your bot" in refused.value.detail

    await choose_bot(alice, scope.lobby_id, {"kind": "localhost"})
    chosen = await choose_bot(bob, scope.lobby_id, {"kind": "default", "level": 2})
    # Every member sees every member's bot in the lobby snapshot.
    assert [(member["display_name"], member["member_settings"]) for member in chosen["members"]] == [
        ("Alice", {"bot": {"kind": "localhost"}}), ("Bob", {"bot": {"kind": "default", "level": 2}})]
    with pytest.raises(LobbyError) as invalid:
        await bob.set_member_settings(scope, {"bot": {"kind": "default", "level": 9}})
    assert (invalid.value.code, invalid.value.status, invalid.value.detail) == (
        "invalid_member_settings", 422, "kind default needs a level that GET /bots/defaults lists: 1, 2, 3")
    await alice.set_ready(scope, True)
    await bob.set_ready(scope, True)
    await alice.start(scope)

    # The backend plays Bob's seat, so a client refuses to play it too.
    status, _, error = await run_bot(bob, scope.lobby_id, 60)
    assert status != 0 and "the backend plays this seat with its level 2 default bot" in error, error
    # Alice's localhost bot, run without a browser, plays the game to its end.
    status, output, error = await run_bot(alice, scope.lobby_id, 600)
    assert status == 0, error
    assert "game ended at generation" in output, output

    async with httpx.AsyncClient(base_url=BASE) as http:
        states = []
        for player in (alice, bob):
            response = await http.get(
                f"/game/from-lobby/{scope.lobby_id}/generation",
                headers=await game_auth(player),
            )
            assert response.status_code == 200, response.text
            states.append(response.json())
        # Choosing conflicts once the lobby has started.
        late = await http.put(
            member_settings_path(scope.lobby_id), json={"member_settings": {"bot": {"kind": "localhost"}}},
            headers={"Authorization": f"Bearer {bob.credential}"},
        )
        assert (late.status_code, late.json()["code"]) == (409, "phase_conflict"), late.text
    assert [state["bot"] for state in states] == [{"kind": "localhost"}, {"kind": "default", "level": 2}]
    assert all(state["outcome"] is not None for state in states)
