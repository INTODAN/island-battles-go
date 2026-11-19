"""A game with a player who plays by hand is real-time (#339): each turn ends
after the lobby's turn pace, and the request that finds it over plays it with
the commands that arrived. Alice plays by hand through the API, as the 3D
client does; a level 2 default bot plays Bob's team."""

import asyncio
from datetime import datetime, timezone
from uuid import uuid4

import httpx
import pytest
from integration.test_live import guests as guests_fixture
from test_lobby_binding import GAME, game_auth, started_lobby

guests = guests_fixture
pytestmark = pytest.mark.asyncio


async def wait_until(deadline_at):
    """Sleep until a turn_deadline_at has passed, by this machine's clock."""
    deadline = datetime.fromisoformat(deadline_at.replace("Z", "+00:00"))
    remaining = (deadline - datetime.now(timezone.utc)).total_seconds()
    await asyncio.sleep(max(remaining, 0) + 0.2)


async def test_a_real_time_game_plays_each_turn_at_its_pace(guests):
    alice, _, scope = await started_lobby(
        guests, "island-battles", {"turn_pace_milliseconds": 500},
        bots=({"kind": "manual"}, {"kind": "default", "level": 2}))
    async with httpx.AsyncClient(base_url=GAME, headers=await game_auth(alice)) as http:
        response = await http.get(f"/game/from-lobby/{scope.lobby_id}/generation")
        assert response.status_code == 200, response.text
        first = response.json()
        assert first["bot"] == {"kind": "manual"}
        assert first["turn_pace_milliseconds"] == 500
        assert first["turn_deadline_at"] is not None

        # The hand player's client sends an order as a batch that doesn't
        # complete the turn.
        own = next(castle for castle in first["generation"]["castles"] if castle["team_color"] == first["team_color"])
        target = next(castle for castle in first["generation"]["castles"] if castle["team_color"] != first["team_color"])
        batch = {"game_id": first["game_id"], "generation_id": first["generation_id"], "request_id": str(uuid4()),
                 "complete": False, "commands": [{"command_type": "send_soldier_group", "parameters": {
                     "origin_castle_id": own["id"], "destination_castle_id": target["id"], "soldier_amount": 5}}]}
        response = await http.post("/generation/commands", json=batch)
        assert response.status_code == 200, response.text
        assert response.json()["generation_id"] == first["generation_id"]

        # Once the pace has passed, the deadline call plays the turn with
        # the order, although Alice never completed it.
        await wait_until(first["turn_deadline_at"])
        response = await http.post(f"/game/from-lobby/{scope.lobby_id}/deadline")
        assert response.status_code == 200, response.text
        second = response.json()
        assert second["turn_number"] == 2 and second.get("outcome") is None and not second["submitted"]
        assert any(group["team_color"] == first["team_color"] and group["soldiers"] == 5
                   for group in second["generation"]["soldier_groups"]), second["generation"]["soldier_groups"]

        # A batch for the turn that ended is stale, and names the pace.
        late = dict(batch, request_id=str(uuid4()))
        response = await http.post("/generation/commands", json=late)
        assert response.status_code == 409, response.text
        assert "0.5 s" in response.json()["detail"], response.text

        # Alice sends nothing for a few turns: each passes, and nobody loses.
        response = await http.get(f"/game/from-lobby/{scope.lobby_id}/generation")
        assert response.status_code == 200, response.text
        state = response.json()
        for _ in range(3):
            previous = state["turn_number"]
            await wait_until(state["turn_deadline_at"])
            response = await http.post(f"/game/from-lobby/{scope.lobby_id}/deadline")
            assert response.status_code == 200, response.text
            state = response.json()
            assert state["turn_number"] == previous + 1 and state.get("outcome") is None, state


async def test_a_hand_player_resigns_against_a_bot(guests):
    """A hand player resigns against a default bot (#347): the bot has played
    the turn already, so it plays in the resign request itself, the game ends
    and the lobby reopens for another game."""
    alice, _, scope = await started_lobby(
        guests, "island-battles", {}, bots=({"kind": "manual"}, {"kind": "default", "level": 2}))
    async with httpx.AsyncClient(base_url=GAME, headers=await game_auth(alice)) as http:
        response = await http.post(f"/game/from-lobby/{scope.lobby_id}/resign")
        assert response.status_code == 200, response.text
        state = response.json()
        outcome = state.get("outcome")
        assert outcome is not None and outcome["reason"] == "last_team_standing", state
        assert outcome["summary"].startswith("Team 1 resigned; ") or " resigned; " in outcome["summary"], outcome
        assert [departure["kind"] for departure in outcome["departures"]] == ["resigned"], outcome
        assert outcome["departures"][0]["team_color"] == state["team_color"]
        assert state["departures"] == outcome["departures"]

        response = await http.post(f"/game/from-lobby/{scope.lobby_id}/resign")
        assert response.status_code == 409, response.text
        assert response.json()["type"] == "urn:island-battles:problem:game-ended", response.text
    reopened = await alice.reopen(scope)
    assert reopened.lobby.phase == "waiting" and reopened.lobby.round_ == 2
