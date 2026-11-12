"""``island-battles-bot package``: the bot and its dependencies as one zip.

The zip is what the server's runner Lambdas run, on Linux arm64 with the
``python3.14`` runtime, without installing anything:

- ``island-battles-bot.json`` at the root, the manifest
  ``{"format": 1, "language": "python", "python": "3.14", "entry": "bot:play"}``,
- ``site/``, the bot project and its locked dependencies, installed for
  ``PYTHONPATH=site`` from wheels only.

It runs in the bot's project: it needs ``uv`` and a ``uv.lock``.
"""

from __future__ import annotations

import argparse
import ast
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import zipfile
from pathlib import Path

MANIFEST_NAME = "island-battles-bot.json"
DEFAULT_ENTRY = "bot:play"
DEFAULT_OUTPUT = "dist/island-battles-bot.zip"
PYTHON_VERSION = "3.14"
# uv's name for the Lambda python3.14 runtime on arm64 (glibc 2.34).
PYTHON_PLATFORM = "aarch64-manylinux_2_28"
MAX_ZIP_BYTES = 250 * 1024 * 1024
MAX_UNPACKED_BYTES = 1024 * 1024 * 1024
# Files in the zip carry no build time, so the same bot makes the same zip.
ZIP_TIME = (1980, 1, 1, 0, 0, 0)
LARGEST_FILES = 5


class PackageError(Exception):
    """A reason the package can't be built, in words for the person running it."""


def manifest(entry: str) -> dict[str, object]:
    """The manifest of a Python package."""
    return {
        "format": 1,
        "language": "python",
        "python": PYTHON_VERSION,
        "entry": entry,
    }


def _mebibytes(size: int) -> str:
    return f"{size / (1024 * 1024):.1f} MiB"


def split_entry(entry: str) -> tuple[str, str]:
    """``module:function`` as its two parts."""
    module, separator, function = entry.partition(":")
    names = [*module.split("."), function]
    if not separator or not all(name.isidentifier() for name in names):
        raise PackageError(f"the entry must be module:function, not {entry!r}")
    return module, function


def _module_files(root: Path, module: str) -> list[Path]:
    relative = Path(*module.split("."))
    return [
        base / candidate
        for base in (root, root / "src")
        for candidate in (relative.with_suffix(".py"), relative / "__init__.py")
        if (base / candidate).is_file()
    ]


def _defines(tree: ast.Module, name: str) -> bool:
    for node in tree.body:
        if isinstance(node, ast.FunctionDef | ast.AsyncFunctionDef):
            if node.name == name:
                return True
        elif isinstance(node, ast.Assign):
            if any(
                isinstance(target, ast.Name) and target.id == name
                for target in node.targets
            ):
                return True
        elif isinstance(node, ast.AnnAssign):
            if isinstance(node.target, ast.Name) and node.target.id == name:
                return True
        elif isinstance(node, ast.Import | ast.ImportFrom) and any(
            (alias.asname or alias.name.partition(".")[0]) == name
            for alias in node.names
        ):
            return True
    return False


def check_entry(root: Path, entry: str) -> None:
    """Raise ``PackageError`` unless the project has the entry's module and function."""
    module, function = split_entry(entry)
    files = _module_files(root, module)
    if not files:
        raise PackageError(
            f"the entry {entry} names the module {module}, but the project has no "
            f"{module.replace('.', '/')}.py (or {module.replace('.', '/')}/__init__.py)"
            f" in {root}. Pass the right module with --entry module:function."
        )
    file = files[0]
    try:
        tree = ast.parse(file.read_text(encoding="utf-8"), filename=str(file))
    except (SyntaxError, UnicodeDecodeError) as error:
        raise PackageError(f"{file} can't be read as Python: {error}") from error
    if not _defines(tree, function):
        raise PackageError(
            f"the entry {entry} names the function {function}, but {file.name} "
            f"has no top-level {function}. Pass the right function with "
            "--entry module:function."
        )


def unpacked_files(root: Path) -> list[tuple[str, int]]:
    """Every file under ``root`` as (path relative to it, size), without reading it."""
    found = []
    for directory, _, names in os.walk(root):
        for name in names:
            path = Path(directory, name)
            found.append((path.relative_to(root).as_posix(), path.stat().st_size))
    return found


def _largest(files: list[tuple[str, int]]) -> str:
    biggest = sorted(files, key=lambda item: (-item[1], item[0]))[:LARGEST_FILES]
    return "\n".join(f"  {_mebibytes(size):>10}  {name}" for name, size in biggest)


def check_unpacked_size(files: list[tuple[str, int]]) -> None:
    """Raise ``PackageError`` naming the largest files when they pass 1 GiB."""
    total = sum(size for _, size in files)
    if total > MAX_UNPACKED_BYTES:
        raise PackageError(
            f"the package is {_mebibytes(total)} unpacked; the limit is "
            f"{_mebibytes(MAX_UNPACKED_BYTES)}. The largest files:\n{_largest(files)}"
        )


def check_zip_size(zip_size: int, files: list[tuple[str, int]]) -> None:
    """Raise ``PackageError`` naming the largest files when the zip passes 250 MiB."""
    if zip_size > MAX_ZIP_BYTES:
        raise PackageError(
            f"the zip is {_mebibytes(zip_size)}; the limit is "
            f"{_mebibytes(MAX_ZIP_BYTES)}. The largest files unpacked:\n{_largest(files)}"
        )


def write_zip(entry: str, site: Path, output: Path) -> list[tuple[str, int]]:
    """Write the zip of the manifest and ``site/``; return the files in it."""
    files = unpacked_files(site)
    check_unpacked_size(files)
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = output.with_name(output.name + ".partial")
    try:
        with zipfile.ZipFile(
            temporary, "w", zipfile.ZIP_DEFLATED, compresslevel=6
        ) as archive:
            text = json.dumps(manifest(entry), indent=2) + "\n"
            _add(archive, MANIFEST_NAME, text.encode(), 0o644)
            for name, _ in sorted(files):
                path = site / name
                mode = 0o755 if os.access(path, os.X_OK) else 0o644
                _add(archive, f"site/{name}", path.read_bytes(), mode)
        check_zip_size(temporary.stat().st_size, files)
        temporary.replace(output)
    finally:
        temporary.unlink(missing_ok=True)
    return files


def _add(archive: zipfile.ZipFile, name: str, data: bytes, mode: int) -> None:
    info = zipfile.ZipInfo(name, ZIP_TIME)
    info.compress_type = zipfile.ZIP_DEFLATED
    info.external_attr = (0o100000 | mode) << 16
    archive.writestr(info, data, compresslevel=6)


def _uv(arguments: list[str], cwd: Path, what: str) -> str:
    """Run uv, and raise ``PackageError`` with uv's own message if it fails."""
    if shutil.which("uv") is None:
        raise PackageError(
            "uv is not installed. `package` uses it to install the dependencies "
            "for Linux arm64: https://docs.astral.sh/uv/"
        )
    done = subprocess.run(
        ["uv", *arguments],
        cwd=cwd,
        capture_output=True,
        text=True,
        check=False,
    )
    if done.returncode != 0:
        raise PackageError(f"{what} failed:\n{done.stderr.strip()}")
    return done.stdout


def _no_wheel(message: str) -> str | None:
    """The package uv names when it has no wheel for Linux arm64, if it does."""
    found = re.search(r"attempted to build `([^`]+)`", message) or re.search(
        r"(\S+) was included because|Because (\S+) (?:has no|only has)", message
    )
    return next((group for group in found.groups() if group), None) if found else None


def _install(arguments: list[str], root: Path, what: str) -> None:
    try:
        _uv(arguments, root, what)
    except PackageError as error:
        message = str(error)
        details = message.partition(":\n")[2] or message
        name = _no_wheel(message)
        if (
            name
            or "no wheels with a matching" in message
            or "Building source distributions is disabled" in message
        ):
            who = f"{name} has" if name else "a dependency has"
            raise PackageError(
                f"{who} no wheel for Linux arm64 and Python {PYTHON_VERSION} "
                f"({PYTHON_PLATFORM}), and a package is built from wheels only. "
                f"Use another version of it, or another package.\n\nuv said:\n{details}"
            ) from error
        raise


def package(root: Path, entry: str, output: Path) -> tuple[int, int]:
    """Build the zip of the project in ``root``; return (zip bytes, unpacked bytes)."""
    check_entry(root, entry)
    if not (root / "pyproject.toml").is_file():
        raise PackageError(
            f"{root} has no pyproject.toml; run this in the bot's project"
        )
    if not (root / "uv.lock").is_file():
        raise PackageError(
            f"{root} has no uv.lock; run `uv lock` so the package has locked dependencies"
        )
    output = output.resolve()
    with tempfile.TemporaryDirectory(prefix="island-battles-package-") as work_name:
        work = Path(work_name)
        site = work / "site"
        target = [
            "--target",
            str(site),
            "--python-platform",
            PYTHON_PLATFORM,
            "--python-version",
            PYTHON_VERSION,
            "--no-deps",
        ]
        requirements = work / "requirements.txt"
        _uv(
            [
                "export",
                "--locked",
                "--no-dev",
                "--no-emit-project",
                "--no-hashes",
                "--no-header",
                "--no-annotate",
                "--output-file",
                str(requirements),
            ],
            root,
            "`uv export` (is uv.lock up to date? run `uv lock`)",
        )
        _install(
            [
                "pip",
                "install",
                *target,
                "--only-binary",
                ":all:",
                "-r",
                str(requirements),
            ],
            root,
            "installing the locked dependencies for Linux arm64",
        )
        wheels = work / "wheels"
        _uv(
            ["build", "--wheel", "--out-dir", str(wheels)],
            root,
            "building the bot project's wheel",
        )
        built = sorted(wheels.glob("*.whl"))
        if not built or not all(
            wheel.name.endswith("-none-any.whl") for wheel in built
        ):
            raise PackageError(
                "the bot project builds a wheel that is not pure Python "
                f"({', '.join(wheel.name for wheel in built) or 'none'}); "
                "a package can only hold pure-Python code of its own"
            )
        _uv(
            ["pip", "install", *target, *map(str, built)],
            root,
            "installing the bot project",
        )
        _tidy(site)
        module, _ = split_entry(entry)
        if not (
            (site / Path(*module.split(".")).with_suffix(".py")).is_file()
            or (site / Path(*module.split(".")) / "__init__.py").is_file()
        ):
            raise PackageError(
                f"the module {module} is not in the project's wheel, so the "
                "runner couldn't import it. Check which files the build backend "
                "includes in pyproject.toml."
            )
        if not (site / "island_battles_client" / "worker.py").is_file():
            raise PackageError(
                "the island-battles-python in uv.lock has no worker mode "
                "(island_battles_client/worker.py), which the runner needs. "
                "Upgrade it: `uv lock --upgrade-package island-battles-python`."
            )
        files = write_zip(entry, site, output)
    return output.stat().st_size, sum(size for _, size in files)


def _tidy(site: Path) -> None:
    """Drop what the runner doesn't need: compiled caches and host scripts."""
    shutil.rmtree(site / "bin", ignore_errors=True)
    for cache in site.rglob("__pycache__"):
        shutil.rmtree(cache, ignore_errors=True)


def run(args: argparse.Namespace) -> int:
    """The ``package`` command."""
    try:
        zipped, unpacked = package(Path.cwd(), args.entry, Path(args.output))
    except PackageError as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1
    print(
        f"wrote {args.output}: {_mebibytes(zipped)} zipped, {_mebibytes(unpacked)} "
        f"unpacked, entry {args.entry}, Linux arm64, Python {PYTHON_VERSION}"
    )
    return 0
