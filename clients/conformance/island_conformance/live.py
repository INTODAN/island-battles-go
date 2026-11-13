"""Helpers for the live tier: the runner's view of a started two-player game."""

import asyncio
import os
import time
from dataclasses import dataclass, field
from datetime import datetime
from uuid import uuid4

import httpx

COMMANDS = "/generation/commands"
# The server ends a game with the turn that creates this generation.
MAX_GENERATIONS = 300


async def choose_localhost_bots(
    base_url: str, lobby_id: str, *credentials: str
) -> None:
    """Choose each player's localhost bot in their lobby member settings, which
    every Island Battles lobby member needs before getting ready: the client
    under test and the runner play these seats themselves."""
    async with httpx.AsyncClient(base_url=base_url, timeout=10) as http:
        for credential in credentials:
            response = await http.put(
                f"/lobby/v1/games/island-battles/lobbies/{lobby_id}/member-settings",
                json={"member_settings": {"bot": {"kind": "localhost"}}},
                headers={"Authorization": f"Bearer {credential}"},
            )
            assert response.status_code == 200, response.text


@dataclass
class Seat:
    credential: str
    state: dict = field(default_factory=dict)
    # The player token the game's routes take, and when to get a new one.
    token: str = ""
    renew_at: float = 0.0


@dataclass
class Match:
    http: httpx.AsyncClient
    lobby_id: str
    alice: Seat
    bob: Seat

    @property
    def generation_path(self) -> str:
        return f"/game/from-lobby/{self.lobby_id}/generation"

    async def auth(self, seat: Seat) -> dict[str, str]:
        """The seat's Authorization header for the game's routes: a player
        token from the lobby, renewed after 80 % of its life."""
        if not seat.token or time.time() >= seat.renew_at:
            response = await self.http.post(
                "/lobby/v1/player-tokens",
                json={"game_key": "island-battles"},
                headers={"Authorization": f"Bearer {seat.credential}"},
            )
            assert response.status_code == 200, response.text
            issued = response.json()
            now = time.time()
            expires = datetime.fromisoformat(issued["expires_at"]).timestamp()
            seat.token, seat.renew_at = issued["token"], now + (expires - now) * 0.8
        return {"Authorization": f"Bearer {seat.token}"}

    async def read(self, seat: Seat) -> dict:
        response = await self.http.get(
            self.generation_path, headers=await self.auth(seat)
        )
        assert response.status_code == 200, response.text
        return response.json()

    async def read_logs(self, seat: Seat, generation_id: int) -> dict:
        """Read a generation's game log as the seat may see it."""
        response = await self.http.get(
            f"/game/from-lobby/{self.lobby_id}/logs",
            params={"generation_id": generation_id},
            headers=await self.auth(seat),
        )
        assert response.status_code == 200, response.text
        return response.json()

    async def complete(self, seat: Seat, commands=(), logs=()) -> dict:
        return await self.submit(seat, await self.read(seat), commands, logs)

    async def submit(self, seat: Seat, state: dict, commands=(), logs=()) -> dict:
        """Complete the seat's turn for the generation of state."""
        response = await self.post(seat, state, commands, logs, complete=True)
        assert response.status_code == 200, response.text
        return response.json()

    async def post(
        self, seat: Seat, state: dict, commands=(), logs=(), *, complete: bool
    ) -> httpx.Response:
        """Post one batch for the generation of state and return the raw answer."""
        return await self.http.post(
            COMMANDS,
            headers=await self.auth(seat),
            json={
                "game_id": state["game_id"],
                "generation_id": state["generation_id"],
                "request_id": str(uuid4()),
                "complete": complete,
                "commands": list(commands),
                "logs": list(logs),
            },
        )

    async def play_turns(self, turns: int) -> dict:
        """Play whole turns for both seats over HTTP; return the latest state."""
        state = await self.read(self.alice)
        for _ in range(turns):
            await self.submit(self.alice, state)
            state = await self.submit(self.bob, state)
        return state

    async def wait_submitted(self, seat: Seat, timeout: float = 20) -> dict:
        async with asyncio.timeout(timeout):
            while not (state := await self.read(seat))["submitted"]:
                await asyncio.sleep(0.05)
        return state


def own_castle(state: dict) -> int:
    return next(
        castle["id"]
        for castle in state["generation"]["castles"]
        if castle["team_color"] == state["team_color"]
    )


def enemy_castle(state: dict) -> int:
    return next(
        castle["id"]
        for castle in state["generation"]["castles"]
        if castle["team_color"] != state["team_color"]
    )


def send(origin, destination, amount) -> dict:
    return {
        "type": "send_soldier_group",
        "origin_castle_id": origin,
        "destination_castle_id": destination,
        "soldier_amount": amount,
    }


def upgrade(castle_id, upgrade_type) -> dict:
    return {
        "type": "upgrade_castle",
        "castle_id": castle_id,
        "upgrade_type": upgrade_type,
    }


async def centrifugo(method: str, payload: dict) -> None:
    """Call Centrifugo's server API, e.g. to publish or force a reconnect."""
    async with httpx.AsyncClient(timeout=10) as http:
        response = await http.post(
            f"{os.environ['LOBBY_TEST_CENTRIFUGO_API_URL']}/{method}",
            headers={"X-API-Key": os.environ["LOBBY_TEST_CENTRIFUGO_API_KEY"]},
            json=payload,
        )
    assert response.status_code == 200 and "error" not in response.json(), response.text


async def eventually(predicate, timeout: float = 20) -> None:
    async with asyncio.timeout(timeout):
        while not predicate():
            await asyncio.sleep(0.05)
