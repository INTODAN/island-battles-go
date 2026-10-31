"""Live tier: turn, retry and realtime recovery behavior against the real backend."""

import asyncio
import copy
from uuid import uuid4

from integration.test_live import EVENTS, container

from island_conformance.adapter import AdapterProcess
from island_conformance.live import (
    COMMANDS,
    MAX_GENERATIONS,
    centrifugo,
    enemy_castle,
    eventually,
    own_castle,
    send,
)
from island_conformance.matching import assert_matches as assert_same_board


async def playing(launch, match) -> tuple[AdapterProcess, dict, dict]:
    """Start the client and return it with its start event and first generation."""
    alice = await launch()
    started = await alice.expect("started")
    generation = (await alice.expect("generation"))["generation"]
    return alice, started, generation


def posts(proxy) -> list[dict]:
    return [exchange.body for exchange in proxy.requests("POST", COMMANDS)]


def completions(proxy) -> list[dict]:
    return [body for body in posts(proxy) if body["complete"]]


async def sent(alice: AdapterProcess, order: dict) -> dict:
    return await alice.request(order, ("sent", "send_failed"))


async def test_callback_receives_authoritative_generation_then_commands_and_completion(
    launch, match, proxy
):
    alice, started, generation = await playing(launch, match)
    state = match.alice.state
    assert started["team_color"] == state["team_color"]
    assert_same_board(generation, state["generation"])
    origin, target = own_castle(state), enemy_castle(state)
    for _ in range(2):
        assert (await sent(alice, send(origin, target, 5)))["type"] == "sent"
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    batches = posts(proxy)
    assert [batch["complete"] for batch in batches] == [False, False, True]
    assert [len(batch["commands"]) for batch in batches] == [1, 1, 0]
    assert len({batch["request_id"] for batch in batches}) == 3, (
        "every new batch needs a new request ID"
    )
    assert {(batch["game_id"], batch["generation_id"]) for batch in batches} == {
        (state["game_id"], state["generation_id"])
    }
    assert batches[0]["commands"][0] == {
        "command_type": "send_soldier_group",
        "parameters": {
            "origin_castle_id": origin,
            "destination_castle_id": target,
            "soldier_amount": 5,
        },
    }
    await match.complete(match.bob)
    advanced = (await alice.expect("generation"))["generation"]
    assert advanced["id"] > state["generation_id"]
    assert_same_board(advanced, (await match.read(match.alice))["generation"])
    groups = [
        group
        for group in advanced["soldier_groups"]
        if group["team_color"] == state["team_color"]
    ]
    assert sorted(group["soldiers"] for group in groups) == [5, 5]
    await alice.assert_quiet("generation", 1)


async def test_returning_without_commands_completes_the_turn(launch, match, proxy):
    alice, _, _ = await playing(launch, match)
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    assert [(batch["complete"], batch["commands"]) for batch in posts(proxy)] == [
        (True, [])
    ]


async def test_rejected_commands_reach_the_callback_and_leave_the_turn_open(
    launch, match, proxy
):
    alice, _, _ = await playing(launch, match)
    state = match.alice.state
    forbidden = await sent(alice, send(enemy_castle(state), own_castle(state), 1))
    assert (
        forbidden["type"] == "send_failed"
        and forbidden["error"]["kind"] == "not_owned"
        and forbidden["error"]["status"] == 403
    )
    excessive = await sent(alice, send(own_castle(state), enemy_castle(state), 1000))
    assert (
        excessive["type"] == "send_failed"
        and excessive["error"]["kind"] == "insufficient_soldiers"
        and excessive["error"]["status"] == 409
    )
    missing = await sent(alice, send(own_castle(state), 999, 1))
    assert (
        missing["type"] == "send_failed"
        and missing["error"]["kind"] == "castle_not_found"
        and missing["error"]["status"] == 422
    )
    assert not (await match.read(match.alice))["submitted"]
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)


async def test_invalid_arguments_are_rejected_before_any_request(launch, match, proxy):
    alice, _, _ = await playing(launch, match)
    origin, target = own_castle(match.alice.state), enemy_castle(match.alice.state)
    for order in (
        send(origin, target, 0),
        send(origin, target, -1),
        send(origin, target, 1.5),
        send(origin, target, "1"),
        send(origin, target, True),
        send(-1, target, 1),
        {"type": "send_soldier_group", "origin_castle_id": origin, "soldier_amount": 1},
    ):
        failure = await sent(alice, order)
        assert (
            failure["type"] == "send_failed"
            and failure["error"]["kind"] == "invalid_argument"
        ), order
    assert posts(proxy) == []
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)


async def test_failed_callback_keeps_sent_commands_but_does_not_complete(
    launch, match, proxy
):
    alice, _, _ = await playing(launch, match)
    state = match.alice.state
    assert (await sent(alice, send(own_castle(state), enemy_castle(state), 3)))[
        "type"
    ] == "sent"
    await alice.send({"type": "fail"})
    stopped = await alice.expect("stopped")
    assert stopped["error"]["kind"] == "player"
    # The last batch carries only the callback's game log, with its traceback.
    batches = posts(proxy)
    assert [batch["complete"] for batch in batches] == [False, False]
    assert batches[1]["commands"] == []
    assert not (await match.read(match.alice))["submitted"]


async def test_lost_command_response_is_retried_with_the_same_request(
    launch, match, proxy
):
    alice, _, _ = await playing(launch, match)
    state = match.alice.state
    proxy.drop_responses("POST", COMMANDS)
    assert (await sent(alice, send(own_castle(state), enemy_castle(state), 4)))[
        "type"
    ] == "sent"
    exchanges = proxy.requests("POST", COMMANDS)
    assert len(exchanges) == 2 and exchanges[0].injected == "drop"
    assert exchanges[0].body == exchanges[1].body, (
        "a retry must repeat the request ID and payload"
    )
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    await match.complete(match.bob)
    advanced = (await alice.expect("generation"))["generation"]
    groups = [
        group
        for group in advanced["soldier_groups"]
        if group["team_color"] == state["team_color"]
    ]
    assert [group["soldiers"] for group in groups] == [4], (
        "the retried batch was applied twice"
    )


async def test_lost_completion_response_is_retried_with_the_same_request(
    launch, match, proxy
):
    alice, _, _ = await playing(launch, match)
    proxy.drop_responses("POST", COMMANDS)
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    await eventually(lambda: len(completions(proxy)) == 2)
    first, retry = completions(proxy)
    assert first == retry
    await match.complete(match.bob)
    await alice.expect("generation")
    await alice.assert_quiet("generation", 1)


async def test_command_receiver_expires_when_the_callback_returns(launch, match, proxy):
    alice, _, _ = await playing(launch, match)
    state = match.alice.state
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    expired = await sent(alice, send(own_castle(state), enemy_castle(state), 1))
    assert (
        expired["type"] == "send_failed"
        and expired["error"]["kind"] == "receiver_closed"
    )
    assert len(posts(proxy)) == 1


async def test_closing_during_a_callback_does_not_complete_the_turn(
    launch, match, proxy
):
    alice, _, _ = await playing(launch, match)
    await alice.request({"type": "close"}, "closed")
    assert posts(proxy) == []
    assert not (await match.read(match.alice))["submitted"]


async def test_authentication_failure_stops_the_client_before_any_callback(launch):
    alice = await launch(credential="not-a-valid-credential")
    stopped = await alice.expect("stopped")
    assert (
        stopped["error"]["kind"] == "authentication"
        and stopped["error"]["status"] == 401
    )
    assert all(message["type"] != "generation" for message in alice.received)


async def test_idle_client_does_not_poll(launch, match, proxy):
    alice, _, _ = await playing(launch, match)
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    reads = len(proxy.requests("GET", match.generation_path))
    await asyncio.sleep(3)
    assert len(proxy.requests("GET", match.generation_path)) == reads


async def test_restarted_client_does_not_replay_a_completed_turn(launch, match):
    alice, _, generation = await playing(launch, match)
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    await alice.close()
    restarted = await launch()
    await restarted.expect("started")
    await restarted.assert_quiet("generation", 2)
    await match.complete(match.bob)
    advanced = (await restarted.expect("generation"))["generation"]
    assert advanced["id"] > generation["id"]


async def test_foreign_malformed_and_duplicate_publications_are_ignored(
    launch, match, proxy
):
    alice, _, generation = await playing(launch, match)
    await alice.send({"type": "finish"})
    current = await match.wait_submitted(match.alice)
    future = copy.deepcopy(current)
    future["generation_id"] += 1
    future["generation"]["id"] += 1
    future["submitted"] = False
    other_lobby = {**future, "lobby_id": str(uuid4())}
    for data in (
        "not an event",
        {"schema_version": 1, "type": "lobby.changed"},
        {"schema_version": 2, "type": "game.next_generation", "state": future},
        {"schema_version": 1, "type": "game.next_generation", "state": other_lobby},
        {
            "schema_version": 1,
            "type": "game.next_generation",
            "state": {**current, "submitted": False},
        },
    ):
        await centrifugo("publish", {"channel": current["channel"], "data": data})
    await alice.assert_quiet("generation", 1.5)
    assert len(completions(proxy)) == 1
    await match.complete(match.bob)
    advanced = (await alice.expect("generation"))["generation"]
    assert advanced["id"] == generation["id"] + 1
    await alice.assert_quiet("generation", 1)


async def test_generation_missed_while_offline_is_recovered_through_http(
    launch, match, proxy
):
    alice, _, generation = await playing(launch, match)
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    # The player channel keeps no history, so only an HTTP read can recover
    # this turn; reads also fail temporarily after the socket returns.
    await container("stop", "--time", "1", EVENTS)
    try:
        advanced_state = await match.complete(match.bob)
        proxy.fail_requests("GET", match.generation_path, 503, count=3)
    finally:
        await container("start", EVENTS)
    advanced = (await alice.expect("generation", timeout=45))["generation"]
    assert advanced["id"] == advanced_state["generation_id"] > generation["id"]

    # The retried push can arrive before reconciliation finishes; reads must
    # still be retried until one succeeds.
    def reconciled() -> bool:
        reads = proxy.requests("GET", match.generation_path)
        return (
            sum(read.injected == "fail" for read in reads) == 3
            and reads[-1].status == 200
        )

    await eventually(reconciled)
    # The publisher's retry may deliver the same generation again after
    # reconnect; neither the open nor the completed turn may run twice.
    await alice.assert_quiet("generation", 2)
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    await alice.assert_quiet("generation", 2)


async def test_stale_read_cannot_reopen_a_completed_turn(launch, match, proxy):
    alice, _, generation = await playing(launch, match)
    forwarded, release = proxy.hold_response("GET", match.generation_path)
    # Reconnecting makes the client reconcile; its read is answered after it completes.
    await centrifugo(
        "disconnect",
        {
            "user": match.alice.state["player_id"],
            "disconnect": {"code": 4000, "reason": "conformance reconnect"},
        },
    )
    await asyncio.wait_for(forwarded.wait(), 20)
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    await eventually(lambda: len(completions(proxy)) == 1)
    release.set()
    await alice.assert_quiet("generation", 1.5)
    assert len(completions(proxy)) == 1
    await match.complete(match.bob)
    advanced = (await alice.expect("generation"))["generation"]
    assert advanced["id"] == generation["id"] + 1


async def test_game_end_reports_the_outcome_and_stops_the_client(launch, match, proxy):
    # The first generation exists; play every turn but the last over HTTP.
    await match.play_turns(MAX_GENERATIONS - 2)
    alice, _, generation = await playing(launch, match)
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    final = await match.complete(match.bob)
    assert final["outcome"] == {
        "reason": "generation_cap",
        "winner_team_color": None,
        "summary": "Draw: equal castle counts at the generation limit",
    }
    ended = await alice.expect("ended")
    assert ended["outcome"] == final["outcome"]
    assert ended["generation"]["id"] == final["generation_id"] > generation["id"]
    assert_same_board(ended["generation"], final["generation"])
    # The final generation has no turn: no callback and no completion.
    await alice.assert_quiet("generation", 1.5)
    assert len(completions(proxy)) == 1
    await alice.close()
    # A client that starts on a finished game reports the end without a callback.
    restarted = await launch()
    await restarted.expect("started")
    assert (await restarted.expect("ended"))["outcome"] == final["outcome"]
    await restarted.assert_quiet("generation", 1.5)
    assert len(completions(proxy)) == 1
