@echo off
rem Windows version of package.sh: zip the bot and its locked dependencies for
rem Linux arm64, for the server's runners. Writes dist\island-battles-bot.zip.
rem Extra arguments go to `island-battles-bot package` (for example --output or
rem --entry). It needs network access to download the wheels.
setlocal
cd /d "%~dp0.."
uv run island-battles-bot package --entry bot:play %*
exit /b %ERRORLEVEL%
