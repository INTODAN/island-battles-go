"""Live tier: castle upgrades in a bot's result.

These games have three seats: the client under test plays Alice, and the runner
plays her teammate Carol and their opponent Bob over plain HTTP. Every castle
starts with 30 soldiers; speed levels 2, 3 and 4 cost 10, 20 and 30, and
clash_stop_generations levels 2 and 3 cost 10 and 20.
"""

from dataclasses import dataclass

import httpx
import pytest_asyncio
from integration.test_live import BASE, create
from lobby_client import Scope

from island_conformance.adapter import AdapterProcess
from island_conformance.live import (
    COMMANDS,
    Match,
    Seat,
    enemy_castle,
    own_castle,
    send,
    upgrade,
)

PROBLEM = "urn:island-battles:problem:"


@dataclass
class TeamMatch(Match):
    carol: Seat


@pytest_asyncio.fixture
async def match(guests):
    """Replaces the two-seat match for this module, so launch plays Alice here."""
    alice, carol, bob = (
        await guests("Conformance Alice"),
        await guests("Conformance Carol"),
        await guests("Conformance Bob"),
    )
    room = await create(alice, "island-battles")
    scope = Scope.from_snapshot(room)
    for guest in (carol, bob):
        await guest.join_lobby(game_key=scope.game_key, join_code=room.lobby.join_code)
    await alice.select_team(scope, room.teams[0].id)
    await carol.select_team(scope, room.teams[0].id)
    await bob.select_team(scope, room.teams[1].id)
    for guest in (alice, carol, bob):
        await guest.set_ready(scope, True)
    await alice.start(scope)
    async with httpx.AsyncClient(base_url=BASE, timeout=10) as http:
        game = TeamMatch(
            http,
            scope.lobby_id,
            Seat(alice.credential),
            Seat(bob.credential),
            Seat(carol.credential),
        )
        for seat in (game.alice, game.bob, game.carol):
            seat.state = await game.read(seat)
        yield game


async def playing(launch) -> tuple[AdapterProcess, dict]:
    alice = await launch()
    await alice.expect("started")
    return alice, (await alice.expect("generation"))["generation"]


def castle(generation: dict, castle_id: int) -> dict:
    return next(castle for castle in generation["castles"] if castle["id"] == castle_id)


def posts(proxy) -> list[dict]:
    return [exchange.body for exchange in proxy.requests("POST", COMMANDS)]


async def next_generation(
    alice: AdapterProcess, match: TeamMatch, *orders: dict
) -> dict:
    """Finish Alice's turn with the orders, complete the runner's seats, and
    return the generation of Alice's next turn."""
    await alice.send({"type": "finish", "commands": list(orders)})
    await match.wait_submitted(match.alice)
    await match.complete(match.carol)
    await match.complete(match.bob)
    return (await alice.expect("generation"))["generation"]


async def test_an_upgrade_pays_its_level_and_a_group_carries_the_levels(
    launch, match, proxy
):
    alice, generation = await playing(launch)
    own, enemy = own_castle(match.alice.state), enemy_castle(match.alice.state)
    assert all(
        set(entity["upgrades"]) == {"speed", "clash_stop_generations"}
        for entity in generation["castles"]
    )
    assert castle(generation, own)["upgrades"]["speed"] == 1
    advanced = await next_generation(alice, match, upgrade(own, "speed"))
    assert [(batch["complete"], batch["commands"]) for batch in posts(proxy)] == [
        (
            True,
            [
                {
                    "command_type": "upgrade_castle",
                    "parameters": {"castle_id": own, "upgrade_type": "speed"},
                }
            ],
        )
    ]
    upgraded = castle(advanced, own)
    # 30 soldiers pay 10, then the castle grows by one.
    assert upgraded["upgrades"] == {"speed": 2, "clash_stop_generations": 1}
    assert upgraded["soldiers"] == 21
    # A group leaving the castle carries its levels.
    advanced = await next_generation(alice, match, send(own, enemy, 1))
    group = next(
        group
        for group in advanced["soldier_groups"]
        if group["origin_castle_id"] == own
    )
    assert group["upgrades"] == {"speed": 2, "clash_stop_generations": 1}


async def test_two_upgrades_of_one_type_pay_for_both_levels_down_to_the_last_soldier(
    launch, match
):
    alice, _ = await playing(launch)
    own = own_castle(match.alice.state)
    # 10 + 20 spend all 30 soldiers.
    upgraded = castle(
        await next_generation(
            alice, match, upgrade(own, "speed"), upgrade(own, "speed")
        ),
        own,
    )
    assert upgraded["upgrades"]["speed"] == 3
    assert upgraded["soldiers"] == 1


async def test_a_result_the_server_rejects_is_dropped_whole(launch, match, proxy):
    own, enemy = own_castle(match.alice.state), enemy_castle(match.alice.state)
    # Each result stays within the castle's soldiers until its last command.
    for orders, rejected in (
        (
            [upgrade(own, "clash_stop_generations")] * 3,
            ("max_level", 409),
        ),
        (
            [upgrade(own, "speed"), upgrade(own, "speed"), upgrade(own, "speed")],
            ("insufficient_soldiers", 409),
        ),
        ([send(own, enemy, 25), upgrade(own, "speed")], ("insufficient_soldiers", 409)),
    ):
        alice, _ = await playing(launch)
        await alice.send({"type": "finish", "commands": orders})
        stopped = await alice.expect("stopped")
        assert (stopped["error"]["kind"], stopped["error"]["status"]) == rejected
        assert not (await match.read(match.alice))["submitted"]
        await alice.close()


async def test_invalid_upgrades_are_rejected_before_any_request(launch, match, proxy):
    own = own_castle(match.alice.state)
    for order in (
        upgrade(own, "armor"),
        upgrade(own, 1),
        upgrade(-1, "speed"),
        upgrade(str(own), "speed"),
        upgrade(True, "speed"),
        {"type": "upgrade_castle", "castle_id": own},
        {"type": "upgrade_castle", "upgrade_type": "speed"},
    ):
        alice, _ = await playing(launch)
        await alice.send({"type": "finish", "commands": [order]})
        assert (await alice.expect("stopped"))["error"]["kind"] == "invalid_argument"
        await alice.close()
    assert all(
        not batch["complete"] and not batch["commands"] for batch in posts(proxy)
    )


async def test_server_rejects_what_only_a_teammate_reservation_shows(
    launch, match, proxy
):
    own, enemy = own_castle(match.alice.state), enemy_castle(match.alice.state)
    # Carol buys both clash_stop_generations levels from the shared castle,
    # paying all 30 soldiers, before Alice's bot answers.
    bought = await match.post(
        match.carol,
        match.carol.state,
        [
            {
                "command_type": "upgrade_castle",
                "parameters": {"castle_id": own, "upgrade_type": upgrade_type},
            }
            for upgrade_type in ("clash_stop_generations", "clash_stop_generations")
        ],
        complete=False,
    )
    assert bought.status_code == 200, bought.text
    for order, rejected in (
        (upgrade(own, "clash_stop_generations"), ("max_level", 409)),
        (upgrade(own, "speed"), ("insufficient_soldiers", 409)),
        (send(own, enemy, 1), ("insufficient_soldiers", 409)),
    ):
        alice, _ = await playing(launch)
        await alice.send({"type": "finish", "commands": [order]})
        stopped = await alice.expect("stopped")
        assert (stopped["error"]["kind"], stopped["error"]["status"]) == rejected
        await alice.close()
    assert not (await match.read(match.alice))["submitted"]
    alice, _ = await playing(launch)
    advanced = castle(await next_generation(alice, match), own)
    assert advanced["upgrades"]["clash_stop_generations"] == 3
    assert advanced["soldiers"] == 1


async def test_server_rejections_carry_the_problem_types(match):
    """The runner's raw requests reach the server checks that clients make locally."""
    state = match.alice.state
    own, enemy = own_castle(state), enemy_castle(state)
    for parameters, status, problem in (
        ({"castle_id": own}, 422, None),
        ({"upgrade_type": "speed"}, 422, None),
        ({"castle_id": str(own), "upgrade_type": "speed"}, 422, None),
        ({"castle_id": own, "upgrade_type": "speed", "level": 2}, 422, None),
        ({"castle_id": own, "upgrade_type": "armor"}, 422, "unknown-upgrade-type"),
        ({"castle_id": 999, "upgrade_type": "speed"}, 422, "castle-not-found"),
        ({"castle_id": enemy, "upgrade_type": "speed"}, 403, "castle-not-owned"),
    ):
        response = await match.post(
            match.alice,
            state,
            [{"command_type": "upgrade_castle", "parameters": parameters}],
            complete=False,
        )
        assert response.status_code == status, (parameters, response.text)
        expected = PROBLEM + problem if problem else "about:blank"
        assert response.json().get("type", "about:blank") == expected, response.text
    batch = [
        {
            "command_type": "upgrade_castle",
            "parameters": {"castle_id": own, "upgrade_type": upgrade_type},
        }
        for upgrade_type in ("speed", "speed", "speed")
    ]
    response = await match.post(match.alice, state, batch, complete=False)
    assert response.status_code == 409
    assert response.json()["type"] == PROBLEM + "insufficient-soldiers"
    assert "speed level 4 costs 30" in response.json()["detail"]
    batch = [
        {
            "command_type": "upgrade_castle",
            "parameters": {"castle_id": own, "upgrade_type": "clash_stop_generations"},
        }
    ] * 3
    response = await match.post(match.alice, state, batch, complete=False)
    assert response.status_code == 409
    assert response.json()["type"] == PROBLEM + "upgrade-max-level"
    # Every rejected batch was dropped whole: the castle still affords everything.
    response = await match.post(match.alice, state, batch[:2], complete=True)
    assert response.status_code == 200, response.text
