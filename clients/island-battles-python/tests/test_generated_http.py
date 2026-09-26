"""Contract integration checks beyond the shared runtime scenarios."""

import json
from pathlib import Path
from uuid import UUID

import httpx
import pytest
from island_game_api_client.api.island_battles import get_island_lobby_generation
from island_game_api_client.client import AuthenticatedClient
from island_game_api_client.models import GenerationState as WireGenerationState

from island_battles_client import GameProtocolError
from island_battles_client._protocol import GenerationState

FIXTURE = Path(__file__).resolve().parents[3] / "clients/conformance/state.json"


def fixture():
    return json.loads(FIXTURE.read_text())


async def test_generated_operation_authenticates_and_returns_wire_models():
    state = fixture()

    def handle(request):
        assert request.headers["Authorization"] == "Bearer credential"
        assert request.url.path == f"/game/from-lobby/{state['lobby_id']}/generation"
        return httpx.Response(200, json=state)

    api = AuthenticatedClient(
        base_url="http://test",
        token="credential",
        httpx_args={"transport": httpx.MockTransport(handle)},
    )
    async with api:
        response = await get_island_lobby_generation.asyncio_detailed(
            UUID(state["lobby_id"]), client=api
        )
    assert isinstance(response.parsed, WireGenerationState)
    assert response.parsed.to_dict() == state


@pytest.mark.parametrize("bad", [True, 1.0, "1", 0, 2])
def test_event_version_is_strict_before_generated_enum_conversion(bad):
    with pytest.raises(GameProtocolError):
        GenerationState.parse_event(
            {"type": "game.next_generation", "schema_version": bad, "state": fixture()}
        )


@pytest.mark.parametrize("bad", [True, "1", None, float("inf"), float("nan")])
def test_contract_validation_rejects_malformed_coordinates(bad):
    state = fixture()
    state["generation"]["castles"][0]["x"] = bad
    with pytest.raises(GameProtocolError):
        GenerationState.parse(state)
