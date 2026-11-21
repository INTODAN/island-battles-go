"""Where the league keeps its files."""

from pathlib import Path

LEAGUE = Path(__file__).resolve().parents[1]
REPOSITORY = LEAGUE.parent
BOTS = LEAGUE / "bots"
MAPS = LEAGUE / "maps"
RUNS = LEAGUE / "runs"
CACHE = RUNS / "cache"
LEDGER = LEAGUE / "ledger"
ENGINE = LEAGUE / ".bin" / "headless-match"
