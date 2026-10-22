"""Helpers for the live tier: the runner's view of a started two-player game."""

import asyncio
import os
from dataclasses import dataclass, field
from uuid import uuid4

import httpx

COMMANDS = "/generation/commands"


@dataclass
class Seat:
    credential: str
    state: dict = field(default_factory=dict)


@dataclass
class Match:
    http: httpx.AsyncClient
    lobby_id: str
    alice: Seat
    bob: Seat

    @property
    def generation_path(self) -> str:
        return f"/game/from-lobby/{self.lobby_id}/generation"

    async def read(self, seat: Seat) -> dict:
        response = await self.http.get(
            self.generation_path, headers={"Authorization": f"Bearer {seat.credential}"}
        )
        assert response.status_code == 200, response.text
        return response.json()

    async def complete(self, seat: Seat, commands=()) -> dict:
        state = await self.read(seat)
        response = await self.http.post(
            COMMANDS,
            headers={"Authorization": f"Bearer {seat.credential}"},
            json={
                "game_id": state["game_id"],
                "generation_id": state["generation_id"],
                "request_id": str(uuid4()),
                "complete": True,
                "commands": list(commands),
            },
        )
        assert response.status_code == 200, response.text
        return response.json()

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
