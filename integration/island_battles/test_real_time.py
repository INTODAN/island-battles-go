"""A game with a player who plays by hand is real-time (#339): each turn ends
after the lobby's turn pace, and the request that finds it over plays it with
the commands that arrived. Alice plays by hand through the API, as the 3D
client does; a level 2 default bot plays Bob's team.

Nothing here depends on when the first read comes: a request plays at most
one overdue turn, and the next turn starts when its generation is created,
so each check compares with the state the previous request returned. The
pace is 2 s, and the order is sent right after the deadline call that opened
its turn, so it arrives within the turn; a machine that stalls longer gets a
stale batch, which is sent again once for the next turn."""

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
        guests, "island-battles", {"turn_pace_milliseconds": 2000},
        bots=({"kind": "manual"}, {"kind": "default", "level": 2}))
    async with httpx.AsyncClient(base_url=GAME, headers=await game_auth(alice)) as http:
        response = await http.get(f"/game/from-lobby/{scope.lobby_id}/generation")
        assert response.status_code == 200, response.text
        first = response.json()
        assert first["bot"] == {"kind": "manual"}
        assert first["turn_pace_milliseconds"] == 2000
        assert first["turn_deadline_at"] is not None

        # The deadline call after the first turn's end opens a fresh turn,
        # created by that request, so the order sent right after it has the
        # whole pace to arrive.
        await wait_until(first["turn_deadline_at"])
        response = await http.post(f"/game/from-lobby/{scope.lobby_id}/deadline")
        assert response.status_code == 200, response.text
        opened = response.json()
        assert opened["turn_number"] == first["turn_number"] + 1, (first["turn_number"], opened["turn_number"])

        # The hand player's client sends an order as a batch that doesn't
        # complete the turn. On a machine that stalls longer than the pace the
        # turn may end first: the batch is then stale, and is sent once more
        # for the next turn.
        for attempt in range(2):
            own = next(castle for castle in opened["generation"]["castles"] if castle["team_color"] == opened["team_color"])
            target = next(castle for castle in opened["generation"]["castles"] if castle["team_color"] != opened["team_color"])
            batch = {"game_id": opened["game_id"], "generation_id": opened["generation_id"], "request_id": str(uuid4()),
                     "complete": False, "commands": [{"command_type": "send_soldier_group", "parameters": {
                         "origin_castle_id": own["id"], "destination_castle_id": target["id"], "soldier_amount": 5}}]}
            response = await http.post("/generation/commands", json=batch)
            if response.status_code == 200:
                break
            assert attempt == 0 and response.status_code == 409, response.text
            assert response.json()["type"] == "urn:island-battles:problem:stale-generation", response.text
            response = await http.get(f"/game/from-lobby/{scope.lobby_id}/generation")
            assert response.status_code == 200, response.text
            opened = response.json()
        assert response.json()["generation_id"] == opened["generation_id"]

        # Once the pace has passed, the deadline call plays the turn with
        # the order, although Alice never completed it.
        await wait_until(opened["turn_deadline_at"])
        response = await http.post(f"/game/from-lobby/{scope.lobby_id}/deadline")
        assert response.status_code == 200, response.text
        second = response.json()
        assert second["turn_number"] == opened["turn_number"] + 1, (opened["turn_number"], second["turn_number"])
        assert second.get("outcome") is None and not second["submitted"]
        assert any(group["team_color"] == opened["team_color"] and group["soldiers"] == 5
                   for group in second["generation"]["soldier_groups"]), second["generation"]["soldier_groups"]

        # A batch for the turn that ended is stale, and names the pace.
        late = dict(batch, request_id=str(uuid4()))
        response = await http.post("/generation/commands", json=late)
        assert response.status_code == 409, response.text
        assert response.json()["type"] == "urn:island-battles:problem:stale-generation", response.text
        assert "2 s" in response.json()["detail"], response.text

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
