"""Live tier: the bot's game log, sent with each generation's requests."""

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
    sent = await alice.request(
        send(own_castle(state), enemy_castle(state), 2), ("sent", "send_failed")
    )
    assert sent["type"] == "sent"
    await log(alice, "warning", "low on soldiers")
    await log(alice, "error", "giving up")
    await alice.send({"type": "finish"})
    await match.wait_submitted(match.alice)
    # Each request carries the lines logged since the previous one.
    batches = posts(proxy)
    assert [
        [entry["message"] for entry in batch.get("logs", [])] for batch in batches
    ] == [
        ["reading the board", "attacking castle"],
        ["low on soldiers", "giving up"],
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
    assert lines[99:] == [
        line(
            "error",
            f"game log limit reached for generation {generation['id']}: "
            "100 lines per generation; 6 lines were not stored",
            SDK_LOGGER,
        )
    ]
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
