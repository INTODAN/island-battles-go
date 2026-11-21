"""The league's tools: ratings, the tournament's summary, the digest and the roster."""

import json
from pathlib import Path

import pytest

from league_tools import digest, maps, ratings, roster, tournament


def test_ratings_put_the_winner_above_and_level3_at_1000() -> None:
    games = [("strong", "level3", 1.0)] * 6 + [("level3", "weak", 1.0)] * 6 + [("strong", "weak", 0.5)] * 2
    fitted = ratings.fit(games)
    assert fitted["level3"] == pytest.approx(1000)
    assert fitted["strong"] > fitted["level3"] > fitted["weak"]


def test_ratings_do_not_depend_on_the_order_of_the_games() -> None:
    games = [("a", "b", 1.0), ("b", "c", 1.0), ("c", "a", 0.5), ("a", "b", 0.0)]
    assert ratings.fit(games) == pytest.approx(ratings.fit(games[::-1]))


def result(first: str, second: str, winner: str | None, map_name: str = "strait") -> dict:
    seat = {"failed_turns": 0, "slowest_turn_milliseconds": 1.0, "failures": []}
    return {
        "id": f"{first}-{second}-{map_name}",
        "map": map_name,
        "players": [first, second],
        "winner": winner,
        "generations": 40,
        "outcome": {"summary": "someone wins"},
        "seats": [{"name": first, **seat}, {"name": second, **seat}],
        "demo": "demo.json.gz",
    }


def test_summary_counts_points_from_both_seats() -> None:
    results = [result("a", "b", "a"), result("b", "a", "a"), result("a", "level3", None)]
    results.append({"id": "x", "map": "strait", "players": ["a", "b"], "error": "no map"})
    summary = tournament.summarize(results)
    rows = {row["player"]: row for row in summary["standings"]}
    assert (rows["a"]["wins"], rows["a"]["draws"], rows["a"]["losses"]) == (2, 1, 0)
    assert summary["pairs"]["b"]["a"] == [0, 2]
    assert summary["maps"]["a"]["strait"] == [2.5, 3]
    assert summary["errors"] == [{"map": "strait", "players": ["a", "b"], "error": "no map"}]
    table = tournament.markdown(summary, results, ["b"])
    assert "Games b didn't win" in table and "NOT PLAYED" in table


def castle(castle_id: int, team_color: int, soldiers: int, x: float, levels: dict | None = None) -> dict:
    upgrades = {"speed": 1, "generation_amount": 1, "burst_potential": 1, "clash_stop_generations": 1}
    return {
        "id": castle_id,
        "team_color": team_color,
        "soldiers": soldiers,
        "x": x,
        "y": 0,
        "upgrades": upgrades | (levels or {}),
        "burst_debt": 0,
    }


def test_digest_reads_the_events_between_boards() -> None:
    group = {
        "id": 0,
        "team_color": 1,
        "soldiers": 20,
        "x": 0.5,
        "y": 0,
        "origin_castle_id": 0,
        "destination_castle_id": 1,
        "clash_radius": 0.25,
        "upgrades": {"speed": 2},
    }
    frames = [
        {"id": 1, "castles": [castle(0, 1, 30, -1), castle(1, 2, 5, 1)], "soldier_groups": [], "clashes": []},
        {
            "id": 2,
            "castles": [castle(0, 1, 1, -1, {"speed": 2}), castle(1, 2, 6, 1)],
            "soldier_groups": [group],
            "clashes": [],
        },
        {
            "id": 3,
            "castles": [castle(0, 1, 2, -1, {"speed": 2}), castle(1, 1, 13, 1)],
            "soldier_groups": [],
            "clashes": [],
        },
    ]
    line = {"level": "info", "logger": "island_battles.player", "message": "all in"}
    demo = {
        "teams": [{"color": 1, "name": "blue", "players": ["blue"]}, {"color": 2, "name": "red", "players": ["red"]}],
        "outcome": {"summary": "blue wins as the last team standing"},
        "frames": frames,
        "logs": [
            {"generation_id": 1, "players": [{"display_name": "blue", "team_color": 1, "lines": [line]}]},
            {"generation_id": 2, "players": []},
            {"generation_id": 3, "players": []},
        ],
    }
    events = digest.events(demo)
    assert (1, "upgrade: blue castle 0 speed 1->2") in events
    assert any(turn == 1 and event.startswith("send: blue 20 soldiers from castle 0") for turn, event in events)
    assert (2, "capture: castle 1 red (6 soldiers) -> blue (13 soldiers)") in events
    assert (2, "eliminated: red") in events
    text = digest.digest(demo, turns=(1, 1))
    assert "log blue [info]: all in" in text and "| 3 | 2 / 15 / 0 / 2 | 0 / 0 / 0 / 0 | 0 |" in text


def test_new_bot_copies_its_parent_as_a_candidate(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(roster, "BOTS", tmp_path)
    (tmp_path / "founder").mkdir()
    (tmp_path / "founder" / "bot.py").write_text("def play(turn): ...\n")
    founder = roster.Bot(name="founder", status="active", parent="", iteration=0, idea="the first")
    monkeypatch.setattr(roster.Bot, "directory", property(lambda bot: tmp_path / bot.name))
    roster.write_bot(founder)
    child = roster.new_bot("founder_fast", "founder", 3, 'a "quoted" idea')
    assert roster.load_bot("founder_fast") == child
    assert (child.status, child.parent, child.iteration) == ("candidate", "founder", 3)
    assert roster.fingerprint(tmp_path / "founder") == roster.fingerprint(tmp_path / "founder_fast")
    with pytest.raises(ValueError):
        roster.new_bot("level4", "founder", 3, "a default bot's name")
    with pytest.raises(ValueError):
        roster.new_bot("founder_fast", "founder", 3, "taken")
    assert json.loads(json.dumps(child.__dict__))["name"] == "founder_fast"


@pytest.mark.parametrize("seed", range(20))
def test_generated_map_is_the_same_from_both_sides(seed: int) -> None:
    generated = maps.generate("made", seed)
    assert generated == maps.generate("made", seed)
    spots = {(castle["x"], castle["y"]): castle for castle in generated["castles"]}
    assert len(spots) == len(generated["castles"])
    for (x, y), castle in spots.items():
        twin = spots[(-x, -y)]
        assert abs(x) <= generated["plane_size_x"] / 2 and abs(y) <= generated["plane_size_y"] / 2
        assert twin["starting_soldier_amount"] == castle["starting_soldier_amount"]
        assert {castle["team_color"], twin["team_color"]} in ({0}, {1, 2})
