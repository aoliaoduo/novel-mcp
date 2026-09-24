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
  "https://go.dev/dl/$pinnedFile",
  "https://dl.google.com/go/$pinnedFile"
)
$tcDir = Join-Path $repo ".toolchain"
$tcExe = Join-Path $tcDir "go\bin\go.exe"
$workDir = Join-Path $repo ".local\go-bootstrap"
$proxy = $env:HTTPS_PROXY
if (-not $proxy) { $proxy = $env:HTTP_PROXY }
if (-not $proxy) { $proxy = $env:ALL_PROXY }

function GoExeVersion($exe) {
  try { $out = (& $exe version 2>&1 | Out-String) } catch { return $null }
  $m = [regex]::Match($out, 'go([0-9.]+)')
  if (-not $m.Success) { return $null }
  return [version]$m.Groups[1].Value
}

function GoToolchainHealthy($exe) {
  $v = GoExeVersion $exe
  if (-not $v) { return $false }
  try { $goroot = (& $exe env GOROOT 2>$null | Out-String).Trim() } catch { return $false }
  if (-not $goroot) { return $false }
  return ((Test-Path (Join-Path $goroot "src\runtime")) -and
          (Test-Path (Join-Path $goroot "src\unsafe\unsafe.go")) -and
          (Test-Path (Join-Path $goroot "pkg\tool")))
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
  if ($v -eq $pinned -and (GoToolchainHealthy $tcExe)) {
    $goExe = $tcExe
    $goCmd = ".\.toolchain\go\bin\go.exe"
    Write-Host ".toolchain go $v already present."
  } else {
    Write-Host ".toolchain is missing/incomplete or differs from pinned ($pinned); re-downloading." -ForegroundColor Yellow
    Remove-Item -Recurse -Force $tcDir
  }
}
if (-not $goExe) {
  if (Test-Path $workDir) { Remove-Item -Recurse -Force $workDir }
  New-Item -ItemType Directory -Force $workDir | Out-Null
  $zip = Join-Path $workDir $pinnedFile
  $extractDir = Join-Path $workDir "extract"
  $done = $false
  $curl = Get-Command curl.exe -ErrorAction SilentlyContinue
  foreach ($url in $mirrors) {
    Write-Host "downloading $url ..."
    try {
      if ($curl) {
        $curlArgs = @('-L', '--fail', '--silent', '--show-error', '--retry', '2',
          '--connect-timeout', '20', '--max-time', '600', '-o', $zip)
        if ($proxy) { $curlArgs += @('-x', $proxy) }
        $curlArgs += $url
        & $curl.Source @curlArgs
        if ($LASTEXITCODE -ne 0) { throw "curl exited with code $LASTEXITCODE" }
      } else {
        $requestArgs = @{ Uri = $url; OutFile = $zip; UseBasicParsing = $true; TimeoutSec = 600 }
        if ($proxy) { $requestArgs.Proxy = $proxy }
        Invoke-WebRequest @requestArgs
      }
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
  # Avoid PowerShell's Expand-Archive progress UI. Windows PowerShell 5.1 can
  # throw IndexOutOfRangeException from Write-Progress when invoked by a
  # non-interactive host (CI/Agent shells), even though the archive is valid.
  # ZipFile performs the same extraction without depending on host UI state.
  if (Test-Path $extractDir) { Remove-Item -Recurse -Force $extractDir }
  Add-Type -AssemblyName System.IO.Compression.FileSystem
  [IO.Compression.ZipFile]::ExtractToDirectory($zip, $extractDir)
  $stagedExe = Join-Path $extractDir "go\bin\go.exe"
  if (-not (GoToolchainHealthy $stagedExe)) {
    Write-Host "ERROR: extracted toolchain is incomplete" -ForegroundColor Red
    exit 1
  }
  New-Item -ItemType Directory -Force $tcDir | Out-Null
  Move-Item -Path (Join-Path $extractDir "go") -Destination (Join-Path $tcDir "go")
  $manifest = [ordered]@{ version = "go$($pinned.ToString())"; filename = $pinnedFile; os = "windows";
    arch = "amd64"; sha256 = $pinnedSha256; size = $pinnedSize; kind = "archive" }
  [IO.File]::WriteAllText((Join-Path $tcDir "download.json"), ($manifest | ConvertTo-Json -Compress))
  Remove-Item -Recurse -Force $workDir
  $v = GoExeVersion $tcExe
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
