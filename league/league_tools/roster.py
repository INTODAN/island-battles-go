"""The league's bots: one directory each under ``bots/``, with a ``bot.toml``."""

from __future__ import annotations

import hashlib
import json
import re
import shutil
import tomllib
from dataclasses import dataclass
from pathlib import Path

from .paths import BOTS

STATUSES = ("candidate", "active", "retired")
NAME = re.compile(r"[a-z][a-z0-9_]*")
ANCHOR = re.compile(r"level([0-9]+)")


@dataclass(frozen=True)
class Bot:
    """One bot of the league, as its ``bot.toml`` describes it."""

    name: str
    status: str
    parent: str
    iteration: int
    idea: str

    @property
    def directory(self) -> Path:
        """The bot's directory."""
        return BOTS / self.name

    @property
    def entry(self) -> str:
        """The bot's ``module:function``, for the SDK's worker."""
        return f"bots.{self.name}.bot:play"


def load_bot(name: str) -> Bot:
    """Read a bot's ``bot.toml``.

    Raises:
        ValueError: The bot is missing, or its file is not a valid ``bot.toml``.
    """
    path = BOTS / name / "bot.toml"
    if not path.is_file() or not (BOTS / name / "bot.py").is_file():
        raise ValueError(f"there is no bot {name!r}: {path.parent} needs bot.py and bot.toml")
    data = tomllib.loads(path.read_text(encoding="utf-8"))
    bot = Bot(
        name=name,
        status=str(data.get("status", "")),
        parent=str(data.get("parent", "")),
        iteration=int(data.get("iteration", 0)),
        idea=str(data.get("idea", "")),
    )
    if bot.status not in STATUSES:
        raise ValueError(f"{path}: status must be one of {', '.join(STATUSES)}")
    return bot


def load_roster() -> list[Bot]:
    """Every bot of the league, by name."""
    names = sorted(path.parent.name for path in BOTS.glob("*/bot.toml"))
    return [load_bot(name) for name in names]


def write_bot(bot: Bot) -> None:
    """Write a bot's ``bot.toml``."""
    lines = [
        "# candidate: new and unproven. active: in the population. retired: no longer",
        "# developed, kept as an opponent.",
        f"status = {json.dumps(bot.status)}",
        "# The bot this one was copied from, or empty for a founder.",
        f"parent = {json.dumps(bot.parent)}",
        "# The league iteration that created it (ledger/iterations.md).",
        f"iteration = {bot.iteration}",
        "# What this bot tries that its parent doesn't, in a sentence.",
        f"idea = {json.dumps(bot.idea)}",
    ]
    (bot.directory / "bot.toml").write_text("\n".join(lines) + "\n", encoding="utf-8")


def new_bot(name: str, parent: str, iteration: int, idea: str) -> Bot:
    """Copy ``parent`` to a new candidate named ``name``.

    Raises:
        ValueError: The name is taken or not a valid bot name.
    """
    if not NAME.fullmatch(name) or ANCHOR.fullmatch(name):
        raise ValueError(
            f"{name!r} is not a bot name: lower-case letters, digits and _, "
            "starting with a letter, and not levelN, which names a default bot"
        )
    if (BOTS / name).exists():
        raise ValueError(f"there is already a bot {name!r}")
    source = load_bot(parent)
    shutil.copytree(source.directory, BOTS / name, ignore=shutil.ignore_patterns("__pycache__"))
    bot = Bot(name=name, status="candidate", parent=parent, iteration=iteration, idea=idea)
    write_bot(bot)
    return bot


def set_status(name: str, status: str) -> Bot:
    """Change a bot's status."""
    if status not in STATUSES:
        raise ValueError(f"status must be one of {', '.join(STATUSES)}")
    current = load_bot(name)
    bot = Bot(
        name=name,
        status=status,
        parent=current.parent,
        iteration=current.iteration,
        idea=current.idea,
    )
    write_bot(bot)
    return bot


def fingerprint(directory: Path) -> str:
    """A hash of every Python file under a directory: the code that plays."""
    digest = hashlib.sha256()
    for path in sorted(directory.rglob("*.py")):
        if "__pycache__" in path.parts:
            continue
        digest.update(path.relative_to(directory).as_posix().encode())
        digest.update(b"\0")
        digest.update(path.read_bytes())
        digest.update(b"\0")
    return digest.hexdigest()
