# Tailscale fix-up: make sure the tray process is running, restart the service if needed,
# and start an interactive login when the node is not authenticated.
#
# Two facts learned the hard way on this machine:
#   1. With the tray process (tailscale-ipn.exe) not running, tailscaled can sit in
#      "Tailscale is starting. Please wait." / BackendState=NoState forever, even though
#      the control plane is reachable (TLS cert is a real Let's Encrypt cert, HTTPS GET 200).
#      Starting the tray process is what flips the state machine to Running.
#   2. Restarting the service needs elevation, so this script is launched through UAC by
#      tailscale-repair.cmd.
#
# Run with: tailscale-repair.cmd  (double-click, then approve the UAC prompt)
$ErrorActionPreference = "Continue"
$ts = "C:\Program Files\Tailscale\tailscale.exe"
$gui = "C:\Program Files\Tailscale\tailscale-ipn.exe"

if (-not (Test-Path $ts)) {
  Write-Host "tailscale.exe not found. Install Tailscale first." -ForegroundColor Red
  Read-Host "Press Enter to close"
  exit 1
}

$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
Write-Host "[1] elevated: $admin"
if (-not $admin) {
  Write-Host "Run this through tailscale-repair.cmd so it can restart the service." -ForegroundColor Yellow
  Read-Host "Press Enter to close"
  exit 1
}

function Get-State {
  $json = (& $ts status --json 2>&1 | Out-String)
  try { return ($json | ConvertFrom-Json) } catch { return $null }
}

Write-Host "[2] tray process (tailscale-ipn.exe):"
if (Get-Process tailscale-ipn -ErrorAction SilentlyContinue) {
  Write-Host "    already running"
} else {
  Write-Host "    not running - starting it (this is what unsticks 'starting')"
  Start-Process $gui -ErrorAction SilentlyContinue
  Start-Sleep -Seconds 8
}

$state = Get-State
Write-Host "    BackendState = $($state.BackendState)"

if ($state.BackendState -ne "Running") {
  Write-Host "[3] restarting the Tailscale service..."
  try { Restart-Service Tailscale -Force -ErrorAction Stop; Write-Host "    restarted." }
  catch { Write-Host "    restart failed: $($_.Exception.Message)" -ForegroundColor Red }

  if (-not (Get-Process tailscale-ipn -ErrorAction SilentlyContinue)) {
    Write-Host "    restarting tray process too"
    Start-Process $gui -ErrorAction SilentlyContinue
  }

  $deadline = (Get-Date).AddSeconds(60)
  while ((Get-Date) -lt $deadline) {
    Start-Sleep -Seconds 3
    $state = Get-State
    Write-Host "    BackendState = $($state.BackendState)"
    if ($state.BackendState -eq "Running" -or $state.BackendState -eq "NeedsLogin") { break }
  }
} else {
  Write-Host "[3] no restart needed"
}

if ($state.BackendState -eq "Running") {
  Write-Host "[4] node is up." -ForegroundColor Green
} elseif ($state.BackendState -eq "NeedsLogin") {
  Write-Host "[4] login required. A URL will be printed below." -ForegroundColor Cyan
  Write-Host "    Open it in a browser, sign in, then come back to this window."
  & $ts up
} else {
  Write-Host "[4] still not Running ($($state.BackendState))." -ForegroundColor Yellow
  if ($state.Health) { Write-Host "    health: $($state.Health -join ' | ')" -ForegroundColor Yellow }
  Write-Host "    If a proxy tool (Clash Party / mihomo) is running, set *.tailscale.com," -ForegroundColor Yellow
  Write-Host "    *.ts.net and *.tailscale.io to DIRECT, then re-run this script." -ForegroundColor Yellow
}

Write-Host ""
Write-Host "[5] final status:"
$state = Get-State
Write-Host "    BackendState = $($state.BackendState)"
if ($state.Self) {
  Write-Host "    DNSName      = $($state.Self.DNSName)"
  Write-Host "    IPv4         = $($state.Self.TailscaleIPs -join ', ')"
}
if ($state.Health) { Write-Host "    Health       = $($state.Health -join ' | ')" -ForegroundColor Yellow }
Write-Host ""
Write-Host "Next: close this window, then double-click dist\novel-mcp.exe"
Write-Host "      (build it first with scripts\build-dev.cmd if needed)"
Read-Host "Press Enter to close"
