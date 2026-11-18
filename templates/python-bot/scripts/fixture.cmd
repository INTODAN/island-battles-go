@echo off
rem Windows version of fixture.sh: write the SDK's fixture turn to a file
rem (default turn.json) for experiments. Edit the copy, then replay it:
rem uv run island-battles-bot check bot:play --turn turn.json
setlocal
cd /d "%~dp0.."
set "output=%~1"
if "%output%"=="" set "output=turn.json"
uv run island-battles-bot fixture > "%output%" || exit /b 1
echo wrote %output%
