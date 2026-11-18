@echo off
rem Windows version of check.sh: format, lint, types, tests, then the SDK check.
rem It stops at the first step that fails.
setlocal
cd /d "%~dp0.."
uv run ruff format --check || exit /b 1
uv run ruff check || exit /b 1
uv run ty check || exit /b 1
uv run pytest || exit /b 1
uv run island-battles-bot check bot:play
exit /b %ERRORLEVEL%
