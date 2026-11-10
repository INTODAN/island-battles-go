"""The fixture turn is the same in the Python and the Go SDK."""

import json
from pathlib import Path

from island_battles_client.cli import DEFAULT_ORIGINS
from island_battles_client.fixture import fixture_turn_data

GO_SDK = Path(__file__).resolve().parents[2] / "island-battles-go"


def test_go_sdk_has_the_same_fixture_turn() -> None:
    go_fixture = json.loads((GO_SDK / "fixture_turn.json").read_text())
    assert go_fixture == fixture_turn_data()


def test_go_sdk_allows_the_same_origins() -> None:
    serve = (GO_SDK / "serve.go").read_text()
    for origin in DEFAULT_ORIGINS:
        assert f'\t"{origin}",\n' in serve
