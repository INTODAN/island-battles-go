"""Live tier: the bot's game log, sent with the turn's batch."""

from island_conformance.adapter import AdapterProcess
from island_conformance.live import (
    COMMANDS,
    MAX_GENERATIONS,
    enemy_castle,
    own_castle,
    send,
)

PLAYER_LOGGER = "island_battles.player"
SDK_LOGGER = "island_battles.sdk"
CUT_NOTE = "… (cut at 500 characters)"


async def playing(launch, match, **options) -> tuple[AdapterProcess, dict]:
    """Start the client and return it with its first generation."""
    alice = await launch(**options)
    await alice.expect("started")
    return alice, (await alice.expect("generation"))["generation"]


async def log(alice: AdapterProcess, level: str, message: str) -> None:
    await alice.request({"type": "log", "level": level, "message": message}, "logged")


def posts(proxy) -> list[dict]:
    return [exchange.body for exchange in proxy.requests("POST", COMMANDS)]


def own_lines(logs: dict) -> list[dict]:
    """The lines of the only visible player, the client's own."""
    assert len(logs["players"]) == 1, logs
    return logs["players"][0]["lines"]


def line(level: str, message: str, logger: str = PLAYER_LOGGER) -> dict:
    return {"level": level, "logger": logger, "message": message}


async def test_lines_are_stored_in_order_with_the_generation_that_logged_them(
    launch, match, proxy
):
    alice, generation = await playing(launch, match)
    state = match.alice.state
    await log(alice, "debug", "reading the board")
    await log(alice, "info", "attacking castle")
    await log(alice, "warning", "low on soldiers")
    await log(alice, "error", "giving up")
    await alice.send(
        {
            "type": "finish",
            "commands": [send(own_castle(state), enemy_castle(state), 2)],
        }
    )
    await match.wait_submitted(match.alice)
    # The turn's one batch carries every line the bot logged, in order.
    [batch] = posts(proxy)
    assert [entry["message"] for entry in batch["logs"]] == [
        "reading the board",
        "attacking castle",
        "low on soldiers",
        "giving up",
    ]
    logs = await match.read_logs(match.alice, generation["id"])
    assert own_lines(logs) == [
        line("debug", "reading the board"),
        line("info", "attacking castle"),
        line("warning", "low on soldiers"),
        line("error", "giving up"),
    ]
    assert logs["players"][0]["display_name"] == "Conformance Alice"

    await match.complete(match.bob)
    advanced = (await alice.expect("generation"))["generation"]
    await log(alice, "info", "second turn")
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    assert own_lines(await match.read_logs(match.alice, advanced["id"])) == [
        line("info", "second turn")
    ]
    assert len(own_lines(await match.read_logs(match.alice, generation["id"]))) == 4


async def test_lines_in_the_result_follow_the_lines_the_bot_logged(launch, match):
    alice, generation = await playing(launch, match)
    await log(alice, "info", "logged while playing")
    await alice.send(
        {"type": "finish", "logs": [{"level": "warning", "message": "returned"}]}
    )
    await match.wait_submitted(match.alice)
    assert own_lines(await match.read_logs(match.alice, generation["id"])) == [
        line("info", "logged while playing"),
        line("warning", "returned"),
    ]


async def test_lines_past_the_limit_are_replaced_by_one_stored_error(
    launch, match, proxy, tmp_path
):
    log_file = tmp_path / "bot.log"
    alice, generation = await playing(launch, match, log_file=str(log_file))
    for index in range(105):
        await log(alice, "info", f"line {index + 1}")
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    # The client never sends a batch the server would reject.
    assert all(len(batch.get("logs", [])) <= 100 for batch in posts(proxy))
    lines = own_lines(await match.read_logs(match.alice, generation["id"]))
    assert lines[:99] == [line("info", f"line {index + 1}") for index in range(99)]
    assert lines[99:] == [limit_error(generation["id"], 6)]
    # The file gets every line and the error once, when the first line is dropped.
    written = log_file.read_text(encoding="utf-8")
    assert "line 105" in written
    assert (
        written.count(f"game log limit reached for generation {generation['id']}") == 1
    )


async def test_a_long_line_is_cut_and_marked(launch, match):
    alice, generation = await playing(launch, match)
    await log(alice, "info", "y" * 600)
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    [stored] = own_lines(await match.read_logs(match.alice, generation["id"]))
    assert stored["message"] == "y" * (500 - len(CUT_NOTE)) + CUT_NOTE
    assert len(stored["message"]) == 500


async def test_a_failed_callback_keeps_its_lines_and_traceback(launch, match, proxy):
    alice, generation = await playing(launch, match)
    await log(alice, "info", "about to fail")
    await alice.send({"type": "fail"})
    assert (await alice.expect("stopped"))["error"]["kind"] == "player"
    batches = posts(proxy)
    assert [(batch["complete"], batch["commands"]) for batch in batches] == [
        (False, [])
    ]
    [first, failure] = own_lines(await match.read_logs(match.alice, generation["id"]))
    assert first == line("info", "about to fail")
    assert failure["level"] == "error" and failure["logger"] == SDK_LOGGER
    assert "Traceback" in failure["message"]
    assert "scripted callback failure" in failure["message"]
    assert len(failure["message"]) <= 500
    assert not (await match.read(match.alice))["submitted"]


def limit_error(generation_id: int, dropped: int) -> dict:
    return line(
        "error",
        f"game log limit reached for generation {generation_id}: "
        f"100 lines per generation; {dropped} lines were not stored",
        SDK_LOGGER,
    )


async def test_text_that_cannot_be_stored_is_replaced(launch, match):
    alice, generation = await playing(launch, match)
    await log(alice, "info", "a NUL \x00 and a lone surrogate \udcff")
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    assert own_lines(await match.read_logs(match.alice, generation["id"])) == [
        line("info", "a NUL \ufffd and a lone surrogate \ufffd")
    ]


async def test_a_restarted_bot_counts_on_from_its_stored_lines(launch, match, proxy):
    first, generation = await playing(launch, match)
    for index in range(60):
        await log(first, "info", f"first run {index + 1}")
    await first.send({"type": "fail"})
    assert (await first.expect("stopped"))["error"]["kind"] == "player"
    await first.close()
    # The failed callback stored its 60 lines and its traceback.
    state = await match.read(match.alice)
    assert not state["submitted"] and state["log_lines_stored"] == 61

    # The developer fixes the bug and starts the bot again for the same turn.
    second = await launch()
    await second.expect("started")
    assert (await second.expect("generation"))["generation"]["id"] == generation["id"]
    sent_before = len(posts(proxy))
    for index in range(50):
        await log(second, "info", f"second run {index + 1}")
    await second.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    # Counting from the stored lines, the client sent only what the server
    # takes: 38 lines and the error in place of the 39th, with the completion.
    [completion] = posts(proxy)[sent_before:]
    assert completion["complete"] is True and len(completion["logs"]) == 39
    lines = own_lines(await match.read_logs(match.alice, generation["id"]))
    assert len(lines) == 100
    assert lines[:60] == [line("info", f"first run {index + 1}") for index in range(60)]
    assert lines[60]["logger"] == SDK_LOGGER and "Traceback" in lines[60]["message"]
    assert lines[61:99] == [
        line("info", f"second run {index + 1}") for index in range(38)
    ]
    assert lines[99] == limit_error(generation["id"], 12)


async def test_lines_the_server_refuses_are_dropped_and_the_turn_completes(
    launch, match, proxy, tmp_path
):
    log_file = tmp_path / "bot.log"
    alice, generation = await playing(launch, match, log_file=str(log_file))
    # Another client of the same player fills the generation's game log.
    other = [line("info", f"other client {index + 1}") for index in range(100)]
    response = await match.post(
        match.alice, match.alice.state, logs=other, complete=False
    )
    assert response.status_code == 200, response.text
    await log(alice, "info", "one line too many")
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    # The refused batch is sent again without its lines, as a new request.
    refused, resent = posts(proxy)[-2:]
    assert refused["logs"] == [line("info", "one line too many")]
    assert refused["complete"] is True and resent["complete"] is True
    assert not resent.get("logs") and resent["commands"] == refused["commands"]
    assert resent["request_id"] != refused["request_id"]
    assert own_lines(await match.read_logs(match.alice, generation["id"])) == other
    # The bot's outputs say why the line is missing.
    written = log_file.read_text(encoding="utf-8")
    assert "one line too many" in written
    assert (
        written.count(f"game log limit reached for generation {generation['id']}") == 1
    )


async def test_the_opponents_lines_are_hidden_until_the_game_ends(launch, match):
    alice, generation = await playing(launch, match)
    await log(alice, "info", "alice's plan")
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    await match.complete(match.bob, logs=[line("info", "bob's plan")])
    alice_view = await match.read_logs(match.alice, generation["id"])
    bob_view = await match.read_logs(match.bob, generation["id"])
    assert own_lines(alice_view) == [line("info", "alice's plan")]
    assert own_lines(bob_view) == [line("info", "bob's plan")]
    assert alice_view["hidden_team_colors"] == [match.bob.state["team_color"]]
    assert bob_view["hidden_team_colors"] == [match.alice.state["team_color"]]
    await alice.close()

    # Two generations exist; play every turn up to the last over HTTP.
    final = await match.play_turns(MAX_GENERATIONS - 2)
    assert final["outcome"] is not None
    for seat in (match.alice, match.bob):
        ended = await match.read_logs(seat, generation["id"])
        assert ended["hidden_team_colors"] == []
        assert {
            player["display_name"]: player["lines"] for player in ended["players"]
        } == {
            "Conformance Alice": [line("info", "alice's plan")],
            "Conformance Bob": [line("info", "bob's plan")],
        }
