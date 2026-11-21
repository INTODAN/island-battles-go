"""Turn a demo into text an agent can study: a timeline, the events and the bots' logs."""

from __future__ import annotations

import gzip
import json
from pathlib import Path
from typing import Any

MAX_EVENTS = 250


def load_demo(path: Path) -> dict[str, Any]:
    """Read a demo file, packed with gzip or not."""
    data = path.read_bytes()
    if data[:2] == b"\x1f\x8b":
        data = gzip.decompress(data)
    return json.loads(data)


def _team_names(demo: dict[str, Any]) -> dict[int, str]:
    names = {0: "neutral"}
    for team in demo["teams"]:
        names[team["color"]] = team["name"]
    return names


def _totals(frame: dict[str, Any], color: int) -> tuple[int, int, int, int]:
    """A team's castles, soldiers in castles, soldiers in groups and production."""
    castles = [castle for castle in frame["castles"] if castle["team_color"] == color]
    groups = [group for group in frame["soldier_groups"] if group["team_color"] == color]
    return (
        len(castles),
        sum(castle["soldiers"] for castle in castles),
        sum(group["soldiers"] for group in groups),
        sum(castle["upgrades"].get("generation_amount", 1) for castle in castles),
    )


def events(demo: dict[str, Any]) -> list[tuple[int, str]]:
    """What happened in each turn, read from the difference between its board and the next."""
    names = _team_names(demo)
    found: list[tuple[int, str]] = []
    frames = demo["frames"]
    standing = {team["color"] for team in demo["teams"]}
    for index in range(len(frames) - 1):
        turn = index + 1
        before, after = frames[index], frames[index + 1]
        castles_before = {castle["id"]: castle for castle in before["castles"]}
        castles_after = {castle["id"]: castle for castle in after["castles"]}
        known_groups = {group["id"] for group in before["soldier_groups"]}
        for castle_id, castle in castles_after.items():
            earlier = castles_before[castle_id]
            for upgrade, level in castle["upgrades"].items():
                if level > earlier["upgrades"].get(upgrade, 1):
                    found.append(
                        (
                            turn,
                            f"upgrade: {names[earlier['team_color']]} castle {castle_id} "
                            f"{upgrade} {earlier['upgrades'].get(upgrade, 1)}->{level}",
                        )
                    )
            if castle["burst_debt"] > earlier["burst_debt"]:
                found.append(
                    (
                        turn,
                        f"burst: {names[castle['team_color']]} castle {castle_id} +{castle['burst_debt']}",
                    )
                )
        for group in after["soldier_groups"]:
            if group["id"] in known_groups:
                continue
            origin = castles_before[group["origin_castle_id"]]
            target = castles_before[group["destination_castle_id"]]
            found.append(
                (
                    turn,
                    f"send: {names[group['team_color']]} {group['soldiers']} soldiers from castle "
                    f"{origin['id']} (left with {castles_after[origin['id']]['soldiers']}) to castle "
                    f"{target['id']} ({names[target['team_color']]}, {target['soldiers']} soldiers), "
                    f"distance {_distance(origin, target):.1f}, speed level {group['upgrades'].get('speed', 1)}",
                )
            )
        for clash in after["clashes"]:
            sides = ", ".join(
                f"{names[participant['team_color']]} group {participant['group_id']} "
                f"{participant['soldiers_before']}->{participant['soldiers_after']}"
                for participant in clash["participants"]
            )
            found.append((turn, f"clash: {sides}"))
        for castle_id, castle in castles_after.items():
            earlier = castles_before[castle_id]
            if castle["team_color"] != earlier["team_color"]:
                found.append(
                    (
                        turn,
                        f"capture: castle {castle_id} {names[earlier['team_color']]} "
                        f"({earlier['soldiers']} soldiers) -> {names[castle['team_color']]} "
                        f"({castle['soldiers']} soldiers)",
                    )
                )
        for color in sorted(standing):
            if _totals(after, color)[:3] == (0, 0, 0):
                standing.discard(color)
                found.append((turn, f"eliminated: {names[color]}"))
    return found


def _distance(first: dict[str, Any], second: dict[str, Any]) -> float:
    return ((first["x"] - second["x"]) ** 2 + (first["y"] - second["y"]) ** 2) ** 0.5


def _board(frame: dict[str, Any], names: dict[int, str]) -> list[str]:
    lines = []
    for castle in frame["castles"]:
        upgrades = " ".join(f"{upgrade}={level}" for upgrade, level in sorted(castle["upgrades"].items()) if level > 1)
        debt = f" debt={castle['burst_debt']}" if castle["burst_debt"] else ""
        lines.append(
            f"    castle {castle['id']} ({castle['x']:g}, {castle['y']:g}) {names[castle['team_color']]}: "
            f"{castle['soldiers']} soldiers{debt} {upgrades}".rstrip()
        )
    for group in frame["soldier_groups"]:
        lines.append(
            f"    group {group['id']} {names[group['team_color']]}: {group['soldiers']} soldiers at "
            f"({group['x']:.2f}, {group['y']:.2f}), castle {group['origin_castle_id']} -> "
            f"{group['destination_castle_id']}"
        )
    return lines


def digest(
    demo: dict[str, Any],
    every: int = 10,
    turns: tuple[int, int] | None = None,
    logs: str = "all",
) -> str:
    """The game as text.

    Args:
        demo: The demo.
        every: The timeline has one row per this many turns.
        turns: A window of turns to show whole: each turn's board, events and
            log lines. Without it, only the timeline and the events are shown.
        logs: In the window, whose log lines to show: a team's name, ``all``
            or ``none``.
    """
    names = _team_names(demo)
    frames = demo["frames"]
    colors = [team["color"] for team in demo["teams"]]
    outcome = demo["outcome"]
    lines = [
        "Teams: " + ", ".join(f"{color} = {names[color]}" for color in colors),
        f"Outcome: {outcome['summary'] if outcome else 'still running'} ({len(frames)} generations)",
        "",
        "Timeline: castles / soldiers in castles / soldiers in groups / production per turn",
        "| Turn | " + " | ".join(names[color] for color in colors) + " | Neutral castles |",
        "|---" * (len(colors) + 2) + "|",
    ]
    shown = sorted({*range(0, len(frames), max(every, 1)), len(frames) - 1})
    for index in shown:
        cells = ["{} / {} / {} / {}".format(*_totals(frames[index], color)) for color in colors]
        neutral = _totals(frames[index], 0)[0]
        lines.append(f"| {index + 1} | " + " | ".join(cells) + f" | {neutral} |")
    leaders = []
    for index, frame in enumerate(frames):
        totals = {color: sum(_totals(frame, color)[1:3]) for color in colors}
        best = max(totals.values())
        ahead = [color for color in colors if totals[color] == best]
        leader = ahead[0] if len(ahead) == 1 else 0
        if not leaders or leaders[-1][1] != leader:
            leaders.append((index + 1, leader))
    lines += [
        "",
        "Most soldiers, from turn: "
        + ", ".join(f"{turn} {names[leader] if leader else 'tied'}" for turn, leader in leaders[-12:]),
    ]
    all_events = events(demo)
    failures = [
        (index + 1, player["display_name"], line["message"])
        for index, entry in enumerate(demo["logs"])
        for player in entry["players"]
        for line in player["lines"]
        if line["level"] == "error"
    ]
    if failures:
        lines += ["", f"Error lines: {len(failures)}. The first:"]
        lines += [f"  turn {turn} {name}: {message}" for turn, name, message in failures[:5]]
    if turns is None:
        lines += ["", f"Events ({len(all_events)}):"]
        lines += [f"  turn {turn}: {event}" for turn, event in all_events[:MAX_EVENTS]]
        if len(all_events) > MAX_EVENTS:
            lines.append(f"  ... {len(all_events) - MAX_EVENTS} more; narrow them with --turns")
        return "\n".join(lines) + "\n"
    first, last = max(turns[0], 1), min(turns[1], len(frames))
    for turn in range(first, last + 1):
        lines += ["", f"Turn {turn}:"]
        lines += _board(frames[turn - 1], names)
        for player in demo["logs"][turn - 1]["players"] if logs != "none" else []:
            if logs not in ("all", player["display_name"]):
                continue
            for line in player["lines"]:
                lines.append(f"    log {player['display_name']} [{line['level']}]: {line['message']}")
        lines += [f"    {event}" for event_turn, event in all_events if event_turn == turn]
    return "\n".join(lines) + "\n"
