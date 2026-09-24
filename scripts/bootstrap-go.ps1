# Bootstrap a Go toolchain for novel-mcp. Idempotent, no admin rights needed.
#
# - Uses a system `go` only when it exactly matches .go-version.
# - Otherwise downloads the pinned toolchain into .toolchain/ (git-ignored scratch).
# - Probes the default module proxy and falls back to goproxy.cn when unreachable.
#
# Usage:
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\bootstrap-go.ps1
# Or double-click scripts\bootstrap-go.cmd (same thing).
$ErrorActionPreference = "Stop"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

# Minimum version comes from go.mod, so it never drifts out of sync.
$reqMatch = (Select-String -Path (Join-Path $repo "go.mod") -Pattern '^go ([0-9.]+)' | Select-Object -First 1).Matches
if (-not $reqMatch) { Write-Host "ERROR: cannot parse go directive from go.mod" -ForegroundColor Red; exit 1 }
$required = [version]$reqMatch.Groups[1].Value

# Official development/release toolchain pin. go.mod remains the minimum supported
# language/toolchain level; .go-version is the exact build toolchain used by releases.
$pinFile = Join-Path $repo ".go-version"
if (-not (Test-Path $pinFile)) { Write-Host "ERROR: missing .go-version" -ForegroundColor Red; exit 1 }
$pinned = [version](Get-Content $pinFile -Raw).Trim()
if ($pinned -lt $required) { Write-Host "ERROR: .go-version ($pinned) is older than go.mod ($required)" -ForegroundColor Red; exit 1 }
# .toolchain/download.json records the same values after a successful download.
$pinnedFile = "go$($pinned.ToString()).windows-amd64.zip"
$pinnedSha256 = "a3911b5e0e1b1053f25ed0675f4c1c6aad1e2bfcf253df2b9be4caabd2edd95d"
$pinnedSize = 78931360
$mirrors = @(
  "https://goproxy.cn/dl/$pinnedFile",
  "https://go.dev/dl/$pinnedFile"
)
$tcDir = Join-Path $repo ".toolchain"
$tcExe = Join-Path $tcDir "go\bin\go.exe"

function GoExeVersion($exe) {
  try { $out = (& $exe version 2>&1 | Out-String) } catch { return $null }
  $m = [regex]::Match($out, 'go([0-9.]+)')
  if (-not $m.Success) { return $null }
  return [version]$m.Groups[1].Value
}

# 1. Resolve the pinned go: exact system match first, then vendored, else download.
$goExe = $null
$goCmd = "go"
$sysGo = Get-Command go -ErrorAction SilentlyContinue
if ($sysGo) {
  $v = GoExeVersion $sysGo.Source
  if ($v -and $v -eq $pinned) {
    $goExe = $sysGo.Source
    Write-Host "system go $v matches pinned release toolchain."
  } else {
    Write-Host "system go ($v) differs from pinned ($pinned); using .toolchain for reproducible builds." -ForegroundColor Yellow
  }
}
if (-not $goExe -and (Test-Path $tcExe)) {
  $v = GoExeVersion $tcExe
  if ($v -eq $pinned) {
    $goExe = $tcExe
    $goCmd = ".\.toolchain\go\bin\go.exe"
    Write-Host ".toolchain go $v already present."
  } else {
    Write-Host ".toolchain go ($v) differs from pinned ($pinned); re-downloading." -ForegroundColor Yellow
    Remove-Item -Recurse -Force $tcDir
  }
}
if (-not $goExe) {
  New-Item -ItemType Directory -Force $tcDir | Out-Null
  $zip = Join-Path $tcDir $pinnedFile
  $done = $false
  foreach ($url in $mirrors) {
    Write-Host "downloading $url ..."
    try {
      Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing -TimeoutSec 600
      $done = $true
      break
    } catch {
      Write-Host "  failed: $($_.Exception.Message)" -ForegroundColor Yellow
    }
  }
  if (-not $done) { Write-Host "ERROR: all mirrors failed" -ForegroundColor Red; exit 1 }
  $actualSize = (Get-Item $zip).Length
  if ($actualSize -ne $pinnedSize) { Write-Host "ERROR: size mismatch ($actualSize != $pinnedSize)" -ForegroundColor Red; exit 1 }
  $actualSha = (Get-FileHash -Path $zip -Algorithm SHA256).Hash.ToLower()
  if ($actualSha -ne $pinnedSha256) { Write-Host "ERROR: sha256 mismatch" -ForegroundColor Red; exit 1 }
  Expand-Archive -Path $zip -DestinationPath $tcDir -Force
  Remove-Item $zip
  $manifest = [ordered]@{ version = "go$($pinned.ToString())"; filename = $pinnedFile; os = "windows";
    arch = "amd64"; sha256 = $pinnedSha256; size = $pinnedSize; kind = "archive" }
  [IO.File]::WriteAllText((Join-Path $tcDir "download.json"), ($manifest | ConvertTo-Json -Compress))
  $v = GoExeVersion $tcExe
  if (-not $v) { Write-Host "ERROR: extracted toolchain does not run" -ForegroundColor Red; exit 1 }
  $goExe = $tcExe
  $goCmd = ".\.toolchain\go\bin\go.exe"
  Write-Host "ready: .toolchain go $v"
}

# 2. Module proxy: proxy.golang.org is unreachable from some networks.
# Probe what GO ITSELF sees (direct connection, no system proxy: Go does not
# read the Windows proxy settings) and switch to goproxy.cn when needed.
$curProxy = (& $goExe env GOPROXY | Out-String).Trim()
if ($curProxy -ne 'https://proxy.golang.org' -and $curProxy -ne 'https://proxy.golang.org,direct') {
  Write-Host "GOPROXY already customized ($curProxy); leaving it."
} elseif ($env:HTTPS_PROXY -or $env:HTTP_PROXY) {
  Write-Host "proxy env vars are set; Go will use them, leaving GOPROXY."
} else {
  Write-Host "probing default Go module proxy without system proxy (10s timeout) ..."
  $proxyOk = $false
  try {
    $handler = New-Object Net.Http.HttpClientHandler
    $handler.UseProxy = $false
    $client = New-Object Net.Http.HttpClient($handler)
    $client.Timeout = [timespan]::FromSeconds(10)
    $task = $client.GetStringAsync('https://proxy.golang.org/golang.org/x/text/@v/list')
    $task.Wait()
    if ($task.Status -eq 'RanToCompletion' -and $task.Result.Length -gt 0) { $proxyOk = $true }
    $client.Dispose()
  } catch { $proxyOk = $false }
  if ($proxyOk) {
    Write-Host "default Go proxy reachable; leaving GOPROXY."
  } else {
    Write-Host "default Go proxy unreachable; switching to https://goproxy.cn,direct." -ForegroundColor Yellow
    & $goExe env -w GOPROXY=https://goproxy.cn,direct
    Write-Host "wrote user Go env (revert any time: go env -u GOPROXY)."
  }
}

Write-Host ""
Write-Host "build:  scripts\\build-dev.cmd  (output: dist\\novel-mcp.exe)"
Write-Host "test:   $goCmd test ./..."
