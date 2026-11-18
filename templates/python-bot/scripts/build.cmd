@echo off
rem Windows version of build.sh: build the bot package (a wheel and a source
rem archive) into dist\.
setlocal
cd /d "%~dp0.."
uv build || exit /b 1
echo package files:
for %%f in (dist\*) do echo %%~ff
