"""Live tier: turn, retry and realtime recovery behavior against the real backend."""

import asyncio
import copy
from datetime import datetime
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


async def finish(alice: AdapterProcess, *orders: dict, memory=None) -> None:
    """Return the bot's result: the orders as commands, and its memory."""
    message = {"type": "finish", "commands": list(orders)}
    if memory is not None:
        message["memory"] = memory
    await alice.send(message)


async def test_bot_receives_the_authoritative_turn_and_answers_with_one_batch(
    launch, match, proxy
):
    alice, started, generation = await playing(launch, match)
    state = match.alice.state
    assert started["team_color"] == state["team_color"]
    assert_same_board(generation, state["generation"])
    turn = alice.received[-1]
    assert turn["team_color"] == state["team_color"]
    assert turn["game_id"] == state["game_id"]
    assert turn["memory"] is None
    # The table is the one the server lists, and every turn has the same one.
    assert (
        turn["upgrades"] == (await match.http.get("/game/upgrades")).json()["upgrades"]
    )
    assert (turn["turn_deadline_at"] is None) == (state.get("turn_deadline_at") is None)
    if turn["turn_deadline_at"] is not None:
        assert datetime.fromisoformat(
            turn["turn_deadline_at"]
        ) == datetime.fromisoformat(state["turn_deadline_at"])
    origin, target = own_castle(state), enemy_castle(state)
    await finish(alice, send(origin, target, 5), send(origin, target, 5))
    await match.wait_submitted(match.alice)
    [batch] = posts(proxy)
    assert batch["complete"] is True
    assert (batch["game_id"], batch["generation_id"]) == (
        state["game_id"],
        state["generation_id"],
    )
    assert (
        batch["commands"]
        == [
            {
                "command_type": "send_soldier_group",
                "parameters": {
                    "origin_castle_id": origin,
                    "destination_castle_id": target,
                    "soldier_amount": 5,
                },
            }
        ]
        * 2
    )
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
    await finish(alice)
    await match.wait_submitted(match.alice)
    assert [(batch["complete"], batch["commands"]) for batch in posts(proxy)] == [
        (True, [])
    ]


async def test_memory_is_stored_with_the_turn_and_given_back_next_turn(
    launch, match, proxy
):
    alice, _, _ = await playing(launch, match)
    await finish(alice, memory={"target": 3, "seen": [1, 2]})
    await match.wait_submitted(match.alice)
    [batch] = posts(proxy)
    assert batch["memory"] == {"target": 3, "seen": [1, 2]}
    await match.complete(match.bob)
    await alice.expect("generation")
    assert alice.received[-1]["memory"] == {"target": 3, "seen": [1, 2]}
    assert (await match.read(match.alice))["memory"] == {"target": 3, "seen": [1, 2]}
    # A turn that returns no memory clears it.
    await finish(alice)
    await match.wait_submitted(match.alice)
    await match.complete(match.bob)
    await alice.expect("generation")
    assert alice.received[-1]["memory"] is None


async def test_memory_over_64_kib_fails_before_any_request(launch, match, proxy):
    alice, _, _ = await playing(launch, match)
    await finish(alice, memory={"blob": "x" * 70_000})
    stopped = await alice.expect("stopped")
    assert stopped["error"]["kind"] == "memory_too_large"
    assert stopped["error"]["status"] is None
    # Only the failed turn's log is sent, with no commands and no memory.
    assert all(
        not batch["complete"] and not batch["commands"] and "memory" not in batch
        for batch in posts(proxy)
    )
    assert not (await match.read(match.alice))["submitted"]


async def test_memory_the_server_refuses_leaves_the_turn_open(launch, match):
    # A server-side rejection reaches the client as the same error, with status 422.
    state = match.alice.state
    response = await match.http.post(
        COMMANDS,
        headers={"Authorization": f"Bearer {match.alice.credential}"},
        json={
            "game_id": state["game_id"],
            "generation_id": state["generation_id"],
            "request_id": str(uuid4()),
            "complete": True,
            "commands": [],
            "memory": {"blob": "x" * 70_000},
        },
    )
    assert response.status_code == 422
    assert response.json()["type"].endswith("bot-memory-too-large")
    assert not (await match.read(match.alice))["submitted"]


async def test_a_result_the_server_rejects_stops_the_client_and_leaves_the_turn_open(
    launch, match, proxy
):
    for order, kind, status in (
        (lambda own, enemy: send(enemy, own, 1), "not_owned", 403),
        (lambda own, enemy: send(own, enemy, 1000), "insufficient_soldiers", 409),
        (lambda own, enemy: send(own, 999, 1), "castle_not_found", 422),
    ):
        alice, _, _ = await playing(launch, match)
        state = match.alice.state
        # The valid command is rejected with the invalid one: the batch is atomic.
        await finish(
            alice,
            send(own_castle(state), enemy_castle(state), 1),
            order(own_castle(state), enemy_castle(state)),
        )
        stopped = await alice.expect("stopped")
        assert (stopped["error"]["kind"], stopped["error"]["status"]) == (kind, status)
        assert not (await match.read(match.alice))["submitted"]
        await alice.close()


async def test_invalid_arguments_are_rejected_before_any_request(launch, match, proxy):
    origin, target = own_castle(match.alice.state), enemy_castle(match.alice.state)
    for order in (
        send(origin, target, 0),
        send(origin, target, 1.5),
        send(origin, target, "1"),
        send(origin, target, True),
        send(-1, target, 1),
        {"type": "send_soldier_group", "origin_castle_id": origin, "soldier_amount": 1},
    ):
        alice, _, _ = await playing(launch, match)
        await finish(alice, order)
        stopped = await alice.expect("stopped")
        assert stopped["error"]["kind"] == "invalid_argument", order
        await alice.close()
    # Only the failed turns' logs were sent: no command, no completion.
    assert all(
        not batch["complete"] and not batch["commands"] for batch in posts(proxy)
    )
    assert not (await match.read(match.alice))["submitted"]


async def test_failed_callback_does_not_complete(launch, match, proxy):
    alice, _, _ = await playing(launch, match)
    await alice.send({"type": "fail"})
    stopped = await alice.expect("stopped")
    assert stopped["error"]["kind"] == "player"
    # The only batch carries the bot's game log, with its traceback.
    [batch] = posts(proxy)
    assert batch["complete"] is False and batch["commands"] == []
    assert not (await match.read(match.alice))["submitted"]


async def test_lost_result_response_is_retried_with_the_same_request(
    launch, match, proxy
):
    alice, _, _ = await playing(launch, match)
    state = match.alice.state
    proxy.drop_responses("POST", COMMANDS)
    await finish(alice, send(own_castle(state), enemy_castle(state), 4))
    await match.wait_submitted(match.alice)
    await eventually(lambda: len(proxy.requests("POST", COMMANDS)) == 2)
    exchanges = proxy.requests("POST", COMMANDS)
    assert len(exchanges) == 2 and exchanges[0].injected == "drop"
    assert exchanges[0].body == exchanges[1].body, (
        "a retry must repeat the request ID and payload"
    )
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


async def test_closing_during_a_turn_does_not_complete_it(launch, match, proxy):
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
