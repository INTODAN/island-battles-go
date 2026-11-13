"""End to end: the Python runner plays a real package of the bot template.

The package is the Python bot template (``templates/python-bot``) packaged
by the SDK's own ``package`` (#216), from this repository's SDK, which has
the worker mode. The runner downloads it, unpacks it, starts the worker and
submits the template bot's answer with the token to a fake game.

``package`` installs the dependencies for Linux arm64, the Lambda's
architecture. On any other Linux machine, such as CI's amd64 runners, the
test packages for this machine's architecture instead, so the compiled
dependencies (pydantic-core) load; the package's layout is the same. It needs
uv, Python 3.14 and PyPI, and is skipped without them or on another system.
"""

from __future__ import annotations

import json
import platform
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.request
from pathlib import Path

import runner
from test_runner import Game, Packages, example_event

REPOSITORY = Path(__file__).resolve().parents[3]
SDK = REPOSITORY / "clients" / "island-battles-python"
TEMPLATE = REPOSITORY / "templates" / "python-bot"

PACKAGE_SCRIPT = """
import sys
from pathlib import Path
from island_battles_client import bundle
if sys.argv[3]:
    bundle.PYTHON_PLATFORM = sys.argv[3]
print(bundle.package(Path(sys.argv[1]), "bot:play", Path(sys.argv[2])))
"""


def network_available() -> bool:
    try:
        urllib.request.urlopen("https://pypi.org/simple/", timeout=5).close()
    except OSError:
        return False
    return True


def host_platform() -> str:
    """uv's platform for this machine's packages, empty for the Lambda's own."""
    machine = platform.machine().lower()
    if machine in ("aarch64", "arm64"):
        return ""
    return f"{machine}-manylinux_2_28"


def run(arguments: list[str], cwd: Path) -> str:
    done = subprocess.run(arguments, cwd=cwd, capture_output=True, text=True, check=False)
    if done.returncode != 0:
        raise AssertionError(f"{' '.join(arguments)} failed:\n{done.stdout}\n{done.stderr}")
    return done.stdout


@unittest.skipUnless(sys.platform == "linux", "the package's dependencies are built for Linux")
@unittest.skipUnless(sys.version_info >= (3, 14), "the runner's Python is 3.14")
@unittest.skipUnless(shutil.which("uv") and network_available(), "needs uv and a network connection to PyPI")
class EndToEndTest(unittest.TestCase):
    def test_plays_a_packaged_template_bot(self) -> None:
        work = Path(tempfile.mkdtemp(prefix="bot-runner-e2e-"))
        self.addCleanup(shutil.rmtree, work, True)
        # The template locks the SDK from PyPI; use this repository's, which
        # has the worker mode, as a wheel, since a package holds wheels only.
        run(["uv", "build", "--wheel", "--out-dir", str(work / "wheels"), str(SDK)], REPOSITORY)
        [wheel] = (work / "wheels").glob("island_battles_python-*.whl")
        project = work / "bot"
        shutil.copytree(TEMPLATE, project, ignore=shutil.ignore_patterns(".venv", "dist", "__pycache__", "uv.lock"))
        pyproject = project / "pyproject.toml"
        text = pyproject.read_text().replace('"island-battles-python>=0.2.0"', '"island-battles-python"')
        self.assertIn('"island-battles-python"', text)
        pyproject.write_text(text + f'\n[tool.uv.sources]\nisland-battles-python = {{ path = "{wheel.as_posix()}" }}\n')
        run(["uv", "lock"], project)
        packaged = work / "island-battles-bot.zip"
        run(
            [
                "uv",
                "run",
                "--project",
                str(SDK),
                "--locked",
                "python",
                "-I",
                "-c",
                PACKAGE_SCRIPT,
                str(project),
                str(packaged),
                host_platform(),
            ],
            REPOSITORY,
        )

        packages, game = Packages(), Game()
        self.addCleanup(packages.server.close)
        self.addCleanup(game.server.close)
        bot_runner = runner.Runner(work / "runner", game.server.host, allow_http_packages=True)
        self.addCleanup(bot_runner.unload)
        event = example_event(
            packages.add("template", packaged.read_bytes()), game.submit_url, load_seconds=10, play_seconds=2
        )

        outcome = bot_runner.handle(event, deadline=time.monotonic() + 20)

        self.assertEqual(outcome, {"outcome": "submitted", "status": 200}, json.dumps(game.submissions)[:2000])
        [submission] = game.submissions
        self.assertEqual(submission["authorization"], "Bearer " + event["delivery"]["submit"]["token"])
        batch = submission["batch"]
        self.assertEqual((batch["game_id"], batch["generation_id"], batch["complete"]), (1, 12, True))
        # The template buys speed where a castle can afford it, then sends
        # half of what is left to the nearest castle: castles 0 and 3 of the
        # example's team, 10 and 1 soldiers, after 4 sent earlier.
        self.assertEqual(
            sorted(command["command_type"] for command in batch["commands"]),
            ["send_soldier_group", "send_soldier_group", "upgrade_castle", "upgrade_castle"],
        )
        self.assertEqual(batch["memory"], {"sent_total": 15})
        self.assertTrue(
            any("4 commands, 15 soldiers sent so far" in line["message"] for line in batch["logs"]), batch["logs"]
        )


if __name__ == "__main__":
    unittest.main()
