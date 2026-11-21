"""Play a round robin between bots, with cached results, and sum it up."""

from __future__ import annotations

import hashlib
import json
import os
import sys
from collections import defaultdict
from dataclasses import dataclass, field
from itertools import combinations
from pathlib import Path
from typing import Any

from . import engine, ratings
from .paths import BOTS, CACHE, LEAGUE, MAPS, REPOSITORY, RUNS
from .roster import ANCHOR, fingerprint, load_bot

SDK = REPOSITORY / "clients" / "island-battles-python" / "island_battles_client"


@dataclass
class Tournament:
    """What to play: every pair of players, on every map, from both sides."""

    players: list[str]
    maps: list[Path]
    # With a focus, only the pairs with a focused player are played.
    focus: list[str] = field(default_factory=list)
    # The players whose lost games and longest wins the tables list.
    listed: list[str] = field(default_factory=list)
    max_generations: int = 300
    turn_timeout_milliseconds: int = 2000
    parallel: int = 0
    use_cache: bool = True


def map_files(selection: str) -> list[Path]:
    """The map files a selection names: ``train``, ``holdout``, ``all`` or names.

    Raises:
        ValueError: A name is not a map of the league, or two maps share a name.
    """
    if selection in ("train", "holdout"):
        files = sorted((MAPS / selection).glob("*.json"))
    elif selection == "all":
        files = sorted(MAPS.glob("*/*.json"))
    else:
        files = []
        for name in selection.split(","):
            found = sorted(MAPS.glob(f"*/{name.strip()}.json"))
            if not found:
                raise ValueError(f"there is no map {name.strip()!r} under {MAPS}")
            files.extend(found)
    names = [file.stem for file in files]
    repeated = sorted({name for name in names if names.count(name) > 1})
    if repeated:
        raise ValueError(f"more than one map is named {', '.join(repeated)}; a map's name is its file's")
    return files


def has_holdout(maps: list[Path]) -> bool:
    """Whether any of the maps is a holdout map, whose games nobody reads."""
    return any(file.parent.name == "holdout" for file in maps)


def player_specification(name: str) -> dict[str, Any]:
    """The runner's description of a player: a default bot or a league bot."""
    anchor = ANCHOR.fullmatch(name)
    if anchor:
        return {"default_level": int(anchor.group(1))}
    return {
        "worker": [sys.executable, "-m", "island_battles_client.worker", load_bot(name).entry],
        "directory": str(LEAGUE),
        # The same hash seed in every process, so that a bot which walks a
        # set plays the same game whichever process plays it.
        "environment": {"PYTHONHASHSEED": "0"},
    }


def player_fingerprint(name: str, sdk: str) -> str:
    """What decides how a player plays: its code, and for a league bot the SDK."""
    if ANCHOR.fullmatch(name):
        return name
    return fingerprint(BOTS / name) + sdk


def match_key(build: str, map_hash: str, seats: list[str], fingerprints: list[str], settings: list[int]) -> str:
    """The name of a match's result in the cache.

    It covers everything that decides the game, and the seats' names, which
    the result carries: two bots with the same code have results of their own.
    """
    return hashlib.sha256(json.dumps([build, map_hash, seats, fingerprints, settings]).encode()).hexdigest()[:32]


def cached_result(key: str) -> dict[str, Any] | None:
    """A match's cached result, when its demo is still there."""
    result_file, demo_file = CACHE / f"{key}.json", CACHE / f"{key}.json.gz"
    if not result_file.is_file() or not demo_file.is_file():
        return None
    return json.loads(result_file.read_text(encoding="utf-8")) | {"demo": str(demo_file)}


def play(tournament: Tournament, run_directory: Path) -> list[dict[str, Any]]:
    """Play the tournament's matches, reusing cached results, and return them all.

    Bots are deterministic, so a match is decided by the runner's build, the
    map, the two bots' code and the settings. A match with the same key as an
    earlier one is not played again. A result with a failed turn is not kept:
    a turn can run out of time because the machine was busy, and the next
    tournament plays that match again.
    """
    build = engine.build()
    sdk = fingerprint(SDK)
    focus = set(tournament.focus)
    pairs = [
        pair
        for pair in combinations(dict.fromkeys(tournament.players), 2)
        if not all(ANCHOR.fullmatch(name) for name in pair) and (not focus or focus & set(pair))
    ]
    settings = [tournament.max_generations, tournament.turn_timeout_milliseconds]
    planned: dict[str, dict[str, Any]] = {}
    for map_file in tournament.maps:
        map_hash = hashlib.sha256(map_file.read_bytes()).hexdigest()
        for pair in pairs:
            for seats in (list(pair), list(pair[::-1])):
                fingerprints = [player_fingerprint(name, sdk) for name in seats]
                key = match_key(build, map_hash, seats, fingerprints, settings)
                planned[key] = {"id": key, "map": map_file.stem, "seats": seats}
    results: dict[str, dict[str, Any]] = {}
    if tournament.use_cache:
        for key in planned:
            cached = cached_result(key)
            if cached is not None:
                results[key] = cached
    to_play = [match for key, match in planned.items() if key not in results]
    print(f"{len(planned)} matches: {len(results)} cached, {len(to_play)} to play", file=sys.stderr)
    if to_play:
        CACHE.mkdir(parents=True, exist_ok=True)
        names = sorted({name for match in to_play for name in match["seats"]})
        schedule = {
            "players": {name: player_specification(name) for name in names},
            "maps": {map_file.stem: str(map_file) for map_file in tournament.maps},
            "matches": to_play,
            "max_generations": tournament.max_generations,
            "turn_timeout_milliseconds": tournament.turn_timeout_milliseconds,
            "parallel": tournament.parallel or os.cpu_count() or 1,
            "demo_directory": str(CACHE),
            "compress_demos": True,
        }
        for line in engine.play(schedule, run_directory / "workers.log"):
            results[line["id"]] = line
            cached = CACHE / f"{line['id']}.json"
            if "error" not in line and not any(seat["failed_turns"] for seat in line["seats"]):
                cached.write_text(json.dumps(line), encoding="utf-8")
            else:
                # The runner has replaced the demo, so an earlier result of
                # this match no longer belongs to it.
                cached.unlink(missing_ok=True)
    return [results[key] for key in planned if key in results]


def summarize(results: list[dict[str, Any]]) -> dict[str, Any]:
    """Standings, the table of every pair, each player's record per map and the problems."""
    record: dict[str, dict[str, Any]] = defaultdict(
        lambda: {
            "games": 0,
            "wins": 0,
            "draws": 0,
            "losses": 0,
            "failed_turns": 0,
            "slowest_turn_milliseconds": 0.0,
        }
    )
    pair_points: dict[str, dict[str, list[float]]] = defaultdict(lambda: defaultdict(lambda: [0, 0]))
    map_points: dict[str, dict[str, list[float]]] = defaultdict(lambda: defaultdict(lambda: [0, 0]))
    games = []
    errors = []
    for result in results:
        if "error" in result:
            errors.append(
                {
                    "map": result["map"],
                    "players": result["players"],
                    "error": result["error"],
                }
            )
            continue
        first, second = result["players"]
        winner = result["winner"]
        first_score = 0.5 if winner is None else float(winner == first)
        games.append((first, second, first_score))
        for name, opponent, score, seat in (
            (first, second, first_score, result["seats"][0]),
            (second, first, 1 - first_score, result["seats"][1]),
        ):
            row = record[name]
            row["games"] += 1
            row["wins" if score == 1 else "losses" if score == 0 else "draws"] += 1
            row["failed_turns"] += seat["failed_turns"]
            row["slowest_turn_milliseconds"] = max(row["slowest_turn_milliseconds"], seat["slowest_turn_milliseconds"])
            pair_points[name][opponent][0] += score
            pair_points[name][opponent][1] += 1
            map_points[name][result["map"]][0] += score
            map_points[name][result["map"]][1] += 1
    rating = ratings.fit(games)
    standings = [
        {
            "player": name,
            "rating": round(rating[name]),
            "score": round((row["wins"] + row["draws"] / 2) / row["games"], 3),
            **row,
        }
        for name, row in sorted(record.items(), key=lambda item: (-rating[item[0]], item[0]))
    ]
    return {
        "standings": standings,
        "pairs": {name: dict(row) for name, row in pair_points.items()},
        "maps": {name: dict(row) for name, row in map_points.items()},
        "errors": errors,
    }


def losses(results: list[dict[str, Any]], player: str) -> list[dict[str, Any]]:
    """The games a player lost or drew, shortest first: the ones worth studying."""
    lost = [
        result
        for result in results
        if "error" not in result and player in result["players"] and result["winner"] != player
    ]
    return sorted(lost, key=lambda result: (result["winner"] is None, result["generations"]))


def longest_wins(results: list[dict[str, Any]], player: str) -> list[dict[str, Any]]:
    """The games a player won, longest first: a slow win hides a weakness."""
    won = [result for result in results if "error" not in result and result["winner"] == player]
    return sorted(won, key=lambda result: -result["generations"])


def game_line(result: dict[str, Any]) -> str:
    """One game of a list: its map, seats, outcome, length and demo."""
    return (
        f"- {result['map']}: {' vs '.join(result['players'])}: {result['outcome']['summary']} "
        f"in {result['generations']} generations: `{result['demo']}`"
    )


def markdown(summary: dict[str, Any], results: list[dict[str, Any]], listed: list[str], list_games: bool) -> str:
    """The summary as Markdown tables.

    Args:
        summary: What ``summarize`` returned.
        results: The matches.
        listed: The players whose lost games and longest wins are listed.
        list_games: False leaves those lists out, as for holdout maps, whose
            games nobody reads.
    """
    lines = [
        "| Player | Rating | Score | W | D | L | Failed turns | Slowest turn (ms) |",
        "|---|---|---|---|---|---|---|---|",
    ]
    order = [row["player"] for row in summary["standings"]]
    for row in summary["standings"]:
        lines.append(
            f"| {row['player']} | {row['rating']} | {row['score']:.3f} | {row['wins']} | {row['draws']} "
            f"| {row['losses']} | {row['failed_turns']} | {row['slowest_turn_milliseconds']:.1f} |"
        )

    def cell(points: list[float] | None) -> str:
        return "" if points is None else f"{points[0]:g}/{points[1]:g}"

    lines += ["", "Points of the row's player against the column's:", ""]
    lines += ["| | " + " | ".join(order) + " |", "|---" * (len(order) + 1) + "|"]
    for name in order:
        cells = [cell(summary["pairs"][name].get(opponent)) for opponent in order]
        lines.append(f"| {name} | " + " | ".join(cells) + " |")
    map_names = sorted({result["map"] for result in results})
    lines += ["", "Points per map:", ""]
    lines += ["| | " + " | ".join(map_names) + " |", "|---" * (len(map_names) + 1) + "|"]
    for name in order:
        cells = [cell(summary["maps"][name].get(map_name)) for map_name in map_names]
        lines.append(f"| {name} | " + " | ".join(cells) + " |")
    for error in summary["errors"]:
        lines.append(f"\nNOT PLAYED {error['map']} {' vs '.join(error['players'])}: {error['error']}")
    if listed and not list_games:
        lines += ["", "Holdout maps were played: their games are not listed, and nobody digests them."]
    for name in listed if list_games else []:
        lines += ["", f"Games {name} didn't win (demo to digest):", ""]
        lines += [game_line(result) for result in losses(results, name)[:20]]
        lines += ["", f"Longest wins of {name}:", ""]
        lines += [game_line(result) for result in longest_wins(results, name)[:5]]
    failures = [
        f"- {result['map']} {' vs '.join(result['players'])}: {seat['name']} turn {failure['turn']}: "
        f"{failure['message'].strip().splitlines()[-1][:300]}"
        for result in results
        if "error" not in result
        for seat in result["seats"]
        for failure in seat["failures"][:1]
    ]
    if failures:
        lines += ["", "Failed turns (the first of each game):", "", *failures[:30]]
    return "\n".join(lines) + "\n"


def run(tournament: Tournament, run_id: str) -> tuple[dict[str, Any], str]:
    """Play a tournament and write its results, summary and tables under ``runs/<run_id>/``."""
    run_directory = RUNS / run_id
    run_directory.mkdir(parents=True, exist_ok=True)
    results = play(tournament, run_directory)
    summary = summarize(results) | {"matches": len(results)}
    table = markdown(summary, results, tournament.listed, not has_holdout(tournament.maps))
    with (run_directory / "results.jsonl").open("w", encoding="utf-8") as file:
        for result in results:
            file.write(json.dumps(result) + "\n")
    (run_directory / "summary.json").write_text(json.dumps(summary, indent=1) + "\n", encoding="utf-8")
    (run_directory / "summary.md").write_text(table, encoding="utf-8")
    return summary, table
