@echo off
setlocal
chcp 65001 >nul
cd /d "%~dp0.."

set "NOPAUSE=0"
if /I "%~1"=="--no-pause" set "NOPAUSE=1"

if exist ".toolchain\go\bin\go.exe" (
  set "GOEXE=.toolchain\go\bin\go.exe"
) else (
  set "GOEXE=go"
  where go >nul 2>nul
  if errorlevel 1 (
    echo ERROR: Go not found.
    echo Run scripts\bootstrap-go.cmd first, then run this file again.
    if "%NOPAUSE%"=="0" pause
    exit /b 1
  )
)

if not exist dist mkdir dist
echo Building dist\novel-mcp.exe ...
"%GOEXE%" build -trimpath -o dist\novel-mcp.exe ./cmd/novel-mcp
if errorlevel 1 (
  echo.
  echo BUILD FAILED
  if "%NOPAUSE%"=="0" pause
  exit /b 1
)
>dist\portable.flag echo novel-mcp portable data mode

echo.
echo READY: dist\novel-mcp.exe
echo DATA:  dist\data\ ^(created on first run^)
echo Double-click that file to start novel-mcp.
if "%NOPAUSE%"=="0" pause
