"""The ``league`` command."""

from __future__ import annotations

import argparse
import gzip
import json
import sys
import time
from pathlib import Path

from . import digest as digest_module
from . import maps, roster, tournament
from .paths import MAPS, RUNS


def _tournament(arguments: argparse.Namespace) -> int:
    bots = roster.load_roster()
    if arguments.bots:
        names = [name.strip() for name in arguments.bots.split(",")]
    else:
        names = [bot.name for bot in bots if bot.status != "candidate"]
    focus = [name.strip() for name in arguments.focus.split(",")] if arguments.focus else []
    names += [name for name in focus if name not in names]
    names += [f"level{level}" for level in arguments.anchors.split(",") if level]
    plan = tournament.Tournament(
        players=names,
        maps=tournament.map_files(arguments.maps),
        focus=focus,
        max_generations=arguments.max_generations,
        turn_timeout_milliseconds=arguments.turn_timeout_milliseconds,
        parallel=arguments.parallel,
        use_cache=not arguments.no_cache,
    )
    run_id = arguments.run_id or time.strftime("%Y%m%d-%H%M%S")
    summary, table = tournament.run(plan, run_id)
    print(table)
    print(f"Written to {RUNS / run_id}", file=sys.stderr)
    failed = [row["player"] for row in summary["standings"] if row["failed_turns"]]
    if arguments.strict and (failed or summary["errors"]):
        print(
            f"league: --strict: {len(summary['errors'])} matches were not played, "
            f"and these players failed turns: {', '.join(failed) or 'none'}",
            file=sys.stderr,
        )
        return 1
    return 0


def _digest(arguments: argparse.Namespace) -> int:
    window = None
    if arguments.turns:
        first, _, last = arguments.turns.partition("-")
        window = (int(first), int(last or first))
    demo = digest_module.load_demo(Path(arguments.demo))
    print(digest_module.digest(demo, arguments.every, window, arguments.logs), end="")
    return 0


def _unpack(arguments: argparse.Namespace) -> int:
    source = Path(arguments.demo)
    target = Path(arguments.output or RUNS / "view" / source.name.removesuffix(".gz"))
    target.parent.mkdir(parents=True, exist_ok=True)
    data = source.read_bytes()
    target.write_bytes(gzip.decompress(data) if data[:2] == b"\x1f\x8b" else data)
    print(target)
    return 0


def _roster(arguments: argparse.Namespace) -> int:
    for bot in roster.load_roster():
        if arguments.json:
            print(json.dumps(bot.__dict__))
        else:
            parent = f" (from {bot.parent})" if bot.parent else ""
            print(f"{bot.name:<24} {bot.status:<10} iteration {bot.iteration}{parent}: {bot.idea}")
    return 0


def _new(arguments: argparse.Namespace) -> int:
    bot = roster.new_bot(arguments.name, arguments.parent, arguments.iteration, arguments.idea)
    print(f"created {bot.directory} as a candidate, copied from {bot.parent}")
    return 0


def _new_map(arguments: argparse.Namespace) -> int:
    if not roster.NAME.fullmatch(arguments.name.replace("-", "_")) or list(MAPS.glob(f"*/{arguments.name}.json")):
        raise ValueError(f"{arguments.name!r} is taken or not a map name: lower-case letters, digits and -")
    target = MAPS / arguments.set / f"{arguments.name}.json"
    target.write_text(json.dumps(maps.generate(arguments.name, arguments.seed), indent=2) + "\n", encoding="utf-8")
    print(target)
    return 0


def _status(arguments: argparse.Namespace) -> int:
    bot = roster.set_status(arguments.name, arguments.status)
    print(f"{bot.name} is now {bot.status}")
    return 0


def main(argv: list[str] | None = None) -> int:
    """Run the ``league`` command."""
    parser = argparse.ArgumentParser(prog="league", description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)

    play = commands.add_parser(
        "tournament",
        help="play every pair of bots on every map from both sides, and print the standings",
    )
    play.add_argument("--bots", help="the bots, by name; default: every bot that is not a candidate")
    play.add_argument(
        "--focus",
        help="play only the pairs with one of these bots (added to the bots), and list the games they didn't win",
    )
    play.add_argument("--anchors", default="1,2,3", help="default bot levels to add; '' for none")
    play.add_argument("--maps", default="train", help="train, holdout, all, or map names")
    play.add_argument("--max-generations", type=int, default=300)
    play.add_argument("--turn-timeout-milliseconds", type=int, default=2000)
    play.add_argument("--parallel", type=int, default=0, help="matches at once; default: the CPUs")
    play.add_argument("--no-cache", action="store_true", help="play every match again")
    play.add_argument("--run-id", help="the directory under runs/; default: the time")
    play.add_argument(
        "--strict",
        action="store_true",
        help="exit with status 1 when a match was not played or a player failed a turn",
    )
    play.set_defaults(handler=_tournament)

    study = commands.add_parser("digest", help="print a demo as a timeline, events and logs")
    study.add_argument("demo")
    study.add_argument("--every", type=int, default=10, help="turns per timeline row")
    study.add_argument("--turns", help="show these turns whole, such as 40-55")
    study.add_argument("--logs", default="all", help="with --turns: a team's name, all or none")
    study.set_defaults(handler=_digest)

    unpack = commands.add_parser("unpack", help="unpack a demo for the demo viewer, and print its path")
    unpack.add_argument("demo")
    unpack.add_argument("--output")
    unpack.set_defaults(handler=_unpack)

    listing = commands.add_parser("roster", help="list the bots")
    listing.add_argument("--json", action="store_true")
    listing.set_defaults(handler=_roster)

    new = commands.add_parser("new", help="copy a bot to a new candidate")
    new.add_argument("name")
    new.add_argument("--parent", required=True)
    new.add_argument("--iteration", type=int, required=True)
    new.add_argument("--idea", required=True)
    new.set_defaults(handler=_new)

    status = commands.add_parser("status", help="set a bot's status")
    status.add_argument("name")
    status.add_argument("status", choices=roster.STATUSES)
    status.set_defaults(handler=_status)

    new_map = commands.add_parser("new-map", help="make a fair two-team map from a seed")
    new_map.add_argument("name")
    new_map.add_argument("--seed", type=int, required=True)
    new_map.add_argument("--set", choices=("train", "holdout"), required=True)
    new_map.set_defaults(handler=_new_map)

    arguments = parser.parse_args(argv)
    try:
        return arguments.handler(arguments)
    except (ValueError, RuntimeError) as error:
        print(f"league: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
