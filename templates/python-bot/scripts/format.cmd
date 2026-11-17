@echo off
rem Windows version of format.sh: format the code and apply the safe lint fixes.
setlocal
cd /d "%~dp0.."
uv run ruff format || exit /b 1
uv run ruff check --fix
exit /b %ERRORLEVEL%
