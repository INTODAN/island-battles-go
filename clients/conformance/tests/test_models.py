"""Offline tier: the client's public models parse wire generations, and their
utilities match island_conformance/reference.py."""

import json
import os
import random

import pytest

from island_conformance import reference
from island_conformance.fixtures import FIXTURES, render
from island_conformance.matching import assert_matches
from island_conformance.random_boards import random_board, without_destination

MODEL_FIXTURES = json.loads(FIXTURES.read_text())
SEED = int(os.environ.get("ISLAND_BATTLES_CONFORMANCE_SEED", "20261023"))
RANDOM_BOARDS = int(os.environ.get("ISLAND_BATTLES_CONFORMANCE_BOARDS", "300"))


def test_fixtures_are_generated_from_the_reference():
    assert FIXTURES.read_text() == render(), (
        "run: uv run python -m island_conformance.fixtures"
    )


@pytest.mark.parametrize("case", MODEL_FIXTURES["valid"], ids=lambda case: case["name"])
async def test_valid_generation_exposes_public_model_and_utilities(adapter, case):
    evaluation = await adapter.request(
        {"type": "evaluate", "id": 1, "generation": case["generation"]}, "evaluation"
    )
    assert evaluation["ok"] is True, case["name"]
    assert_matches(
        evaluation["generation"], case.get("expected_generation", case["generation"])
    )
    for key in ("distances", "relations", "distance_left"):
        assert_matches(evaluation[key], case["expected"][key], key)


async def test_invalid_generations_are_rejected(adapter):
    # One process evaluates every case; each reply is matched by its ID.
    for index, case in enumerate(MODEL_FIXTURES["invalid"]):
        evaluation = await adapter.request(
            {"type": "evaluate", "id": index, "generation": case["generation"]},
            "evaluation",
        )
        assert evaluation["id"] == index
        assert evaluation["ok"] is False, f"accepted invalid generation: {case['name']}"


async def test_random_boards_match_the_reference(adapter):
    rng = random.Random(SEED)
    for index in range(RANDOM_BOARDS):
        wire, public = random_board(rng)
        broken = without_destination(rng, wire) if rng.random() < 0.15 else None
        board = broken or wire
        evaluation = await adapter.request(
            {"type": "evaluate", "id": index, "generation": board}, "evaluation"
        )
        location = f"seed {SEED}, board {index}"
        try:
            assert evaluation["id"] == index
            if broken is not None:
                assert evaluation["ok"] is False, (
                    "accepted a group whose destination is missing"
                )
                continue
            assert evaluation["ok"] is True, "rejected a valid board"
            assert_matches(evaluation["generation"], public, "generation")
            expected = reference.evaluate(public)
            for key in ("distances", "relations", "distance_left"):
                assert_matches(evaluation[key], expected[key], key)
        except AssertionError as error:
            raise AssertionError(
                f"{location}: {error}\nboard: {json.dumps(board)}"
            ) from None
