@echo off
rem Windows version of run.sh: play one seat of a started lobby without a
rem browser, until the game ends.
rem Usage: scripts\run.cmd [TOKEN LOBBY_ID]
rem or set ISLAND_BATTLES_TOKEN and ISLAND_BATTLES_LOBBY_ID. The lobby and the
rem game are separate sites: set ISLAND_BATTLES_GAME_URL and
rem ISLAND_BATTLES_LOBBY_URL (https://island-battles.elidan.org and
rem https://lobby.elidan.org for the official game), or ISLAND_BATTLES_BASE_URL
rem for a server that serves both. The log goes to stdout and is appended to
rem bot.log.
setlocal
cd /d "%~dp0.."
set "token=%~1"
if not defined token set "token=%ISLAND_BATTLES_TOKEN%"
set "lobby_id=%~2"
if not defined lobby_id set "lobby_id=%ISLAND_BATTLES_LOBBY_ID%"
set "urls="
if defined ISLAND_BATTLES_BASE_URL goto base_url
if not defined ISLAND_BATTLES_GAME_URL goto check
if not defined ISLAND_BATTLES_LOBBY_URL goto check
set urls=--game-url "%ISLAND_BATTLES_GAME_URL%" --lobby-url "%ISLAND_BATTLES_LOBBY_URL%"
goto check
:base_url
set urls=--base-url "%ISLAND_BATTLES_BASE_URL%"
:check
if not defined urls goto usage
if not defined token goto usage
if not defined lobby_id goto usage
uv run island-battles-bot run bot:play %urls% "--token=%token%" --lobby-id "%lobby_id%" --log-file bot.log
exit /b %ERRORLEVEL%
:usage
echo usage: scripts\run.cmd TOKEN LOBBY_ID (or set ISLAND_BATTLES_TOKEN, ISLAND_BATTLES_LOBBY_ID), with 1>&2
echo ISLAND_BATTLES_GAME_URL and ISLAND_BATTLES_LOBBY_URL set (https://island-battles.elidan.org and 1>&2
echo https://lobby.elidan.org for the official game), or ISLAND_BATTLES_BASE_URL for one server 1>&2
exit /b 2
