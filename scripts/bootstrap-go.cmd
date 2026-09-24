@echo off
REM One-time Go toolchain bootstrap for novel-mcp (no install, no admin).
setlocal
chcp 65001 >nul
set "PS1=%~dp0bootstrap-go.ps1"
powershell -NoProfile -ExecutionPolicy Bypass -File "%PS1%" %*
echo.
pause
