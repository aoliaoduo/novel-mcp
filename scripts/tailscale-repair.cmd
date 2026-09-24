@echo off
REM Launches tailscale-repair.ps1 elevated (UAC prompt) so it can restart the Tailscale service.
REM One double-click, no typing.
setlocal
set "PS1=%~dp0tailscale-repair.ps1"
powershell -NoProfile -ExecutionPolicy Bypass -Command "Start-Process powershell -Verb RunAs -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','%PS1%'"
