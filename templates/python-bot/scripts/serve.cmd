@echo off
rem Windows version of serve.sh: serve the bot on http://127.0.0.1:41414 for the
rem lobby page's "localhost bot". Extra arguments go to `island-battles-bot serve`
rem (for example --port 41415, for a second localhost bot or a localhost bot
rem seat, --allow-origin or --allow-submit-host).
rem Saving a .py file reloads the bot by itself.
setlocal
cd /d "%~dp0.."
uv run island-battles-bot serve bot:play --allow-origin https://lobby.elidan.org %*
exit /b %ERRORLEVEL%
