"""Build and run the headless match runner, ``backend/cmd/headless-match``."""

from __future__ import annotations

import hashlib
import json
import subprocess
from collections.abc import Iterator
from pathlib import Path
from typing import Any

from .paths import ENGINE, REPOSITORY


def build() -> str:
    """Build the runner, and return a hash that names this build.

    Go rebuilds only what changed, so this takes a moment when nothing did.
    The hash changes whenever the rules or the runner do, which is what makes
    a cached result stale.

    Raises:
        RuntimeError: The runner didn't build.
    """
    ENGINE.parent.mkdir(parents=True, exist_ok=True)
    # Without the commit's stamp and the build's paths, the same sources give
    # the same binary in every commit and checkout, so the cache survives them.
    built = subprocess.run(
        ["go", "build", "-buildvcs=false", "-trimpath", "-o", str(ENGINE), "./backend/cmd/headless-match"],
        cwd=REPOSITORY,
        check=False,
    )
    if built.returncode != 0:
        raise RuntimeError("the headless runner didn't build; see the compiler's messages above")
    return hashlib.sha256(ENGINE.read_bytes()).hexdigest()


def play(schedule: dict[str, Any], worker_log: Path) -> Iterator[dict[str, Any]]:
    """Play a schedule, and yield each match's line as it ends.

    Args:
        schedule: The runner's schedule document.
        worker_log: Where the bots' own stderr goes.

    Raises:
        RuntimeError: The runner refused the schedule or stopped early.
    """
    worker_log.parent.mkdir(parents=True, exist_ok=True)
    with worker_log.open("ab") as errors:
        process = subprocess.Popen(
            [str(ENGINE)],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=errors,
            text=True,
            encoding="utf-8",
        )
        assert process.stdin is not None and process.stdout is not None
        process.stdin.write(json.dumps(schedule))
        process.stdin.close()
        for line in process.stdout:
            if line.strip():
                yield json.loads(line)
        if process.wait() != 0:
            tail = worker_log.read_text(encoding="utf-8", errors="replace")[-2000:]
            raise RuntimeError(f"headless-match stopped with status {process.returncode}:\n{tail}")
