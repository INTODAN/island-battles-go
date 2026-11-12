"""``island-battles-bot package``: the zip of a bot project for the runners."""

import json
import shutil
import subprocess
import urllib.request
import zipfile
from pathlib import Path

import pytest

from island_battles_client import bundle
from island_battles_client.bundle import PackageError

PYPROJECT = """
[project]
name = "fixture-bot"
version = "0.1.0"
requires-python = ">=3.14"
dependencies = ["{dependency}"]

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[tool.hatch.build.targets.wheel]
only-include = ["bot.py"]
"""


def network_available() -> bool:
    try:
        urllib.request.urlopen("https://pypi.org/simple/", timeout=5).close()
    except OSError:
        return False
    return True


# These tests run uv against PyPI: they are skipped without uv or a network.
needs_network = pytest.mark.skipif(
    shutil.which("uv") is None or not network_available(),
    reason="needs uv and a network connection to PyPI",
)


def project(directory: Path, dependency: str) -> None:
    """Write a bot project with one dependency, and lock it."""
    (directory / "pyproject.toml").write_text(PYPROJECT.format(dependency=dependency))
    (directory / "bot.py").write_text("def play(turn): ...\n")
    subprocess.run(["uv", "lock"], cwd=directory, check=True, capture_output=True)


@needs_network
def test_package_layout_and_manifest(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    project(tmp_path, "six==1.17.0")
    # The fixture doesn't depend on the SDK, so stand one in with a worker.
    original = bundle._tidy

    def tidy(site: Path) -> None:
        original(site)
        worker = site / "island_battles_client" / "worker.py"
        worker.parent.mkdir(exist_ok=True)
        worker.write_text("")

    monkeypatch.setattr(bundle, "_tidy", tidy)
    output = tmp_path / "dist" / "bot.zip"
    zipped, unpacked = bundle.package(tmp_path, "bot:play", output)
    assert zipped == output.stat().st_size
    assert unpacked > 0
    with zipfile.ZipFile(output) as archive:
        names = archive.namelist()
        assert names[0] == "island-battles-bot.json"
        assert json.loads(archive.read("island-battles-bot.json")) == {
            "format": 1,
            "language": "python",
            "python": "3.14",
            "entry": "bot:play",
        }
        assert "site/bot.py" in names
        assert "site/six.py" in names
        assert not any("__pycache__" in name for name in names)
        assert all(name == names[0] or name.startswith("site/") for name in names)


@needs_network
def test_a_project_without_the_sdk_worker_is_refused(tmp_path: Path) -> None:
    project(tmp_path, "six==1.17.0")
    output = tmp_path / "bot.zip"
    with pytest.raises(PackageError, match="no worker mode"):
        bundle.package(tmp_path, "bot:play", output)
    assert not output.exists()


@needs_network
def test_a_dependency_without_an_arm64_wheel_is_named(tmp_path: Path) -> None:
    project(tmp_path, "sgmllib3k==1.0.0")
    with pytest.raises(PackageError) as error:
        bundle.package(tmp_path, "bot:play", tmp_path / "bot.zip")
    assert "sgmllib3k" in str(error.value)
    assert "no wheel for Linux arm64" in str(error.value)


def test_a_wrong_entry_is_named(tmp_path: Path) -> None:
    (tmp_path / "bot.py").write_text("def play(turn): ...\nplay_alias = play\n")
    bundle.check_entry(tmp_path, "bot:play")
    bundle.check_entry(tmp_path, "bot:play_alias")
    with pytest.raises(PackageError, match="no top-level other"):
        bundle.check_entry(tmp_path, "bot:other")
    with pytest.raises(PackageError, match="no nothing.py"):
        bundle.check_entry(tmp_path, "nothing:play")
    with pytest.raises(PackageError, match="module:function"):
        bundle.check_entry(tmp_path, "bot")


def test_the_entry_may_be_in_a_package_under_src(tmp_path: Path) -> None:
    (tmp_path / "src" / "mybot").mkdir(parents=True)
    (tmp_path / "src" / "mybot" / "__init__.py").write_text("from .x import play\n")
    bundle.check_entry(tmp_path, "mybot:play")


def test_a_project_without_a_lock_is_refused(tmp_path: Path) -> None:
    (tmp_path / "bot.py").write_text("def play(turn): ...\n")
    (tmp_path / "pyproject.toml").write_text("[project]\nname='x'\nversion='1'\n")
    with pytest.raises(PackageError, match="uv.lock"):
        bundle.package(tmp_path, "bot:play", tmp_path / "bot.zip")


def test_an_oversized_package_names_its_largest_files(tmp_path: Path) -> None:
    site = tmp_path / "site"
    site.mkdir()
    (site / "small.py").write_text("x = 1\n")
    # A sparse file: it has the size without the disk space.
    with (site / "huge.bin").open("wb") as file:
        file.truncate(bundle.MAX_UNPACKED_BYTES + 1)
    output = tmp_path / "bot.zip"
    with pytest.raises(PackageError) as error:
        bundle.write_zip("bot:play", site, output)
    assert "unpacked" in str(error.value)
    assert "huge.bin" in str(error.value)
    assert not output.exists()
    assert not list(tmp_path.glob("*.partial"))


def test_an_oversized_zip_names_its_largest_files() -> None:
    files = [("a.bin", 300 * 1024 * 1024), ("b.py", 10)]
    bundle.check_zip_size(bundle.MAX_ZIP_BYTES, files)
    with pytest.raises(PackageError, match="a.bin"):
        bundle.check_zip_size(bundle.MAX_ZIP_BYTES + 1, files)
