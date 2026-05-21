# install.ps1 — plug-and-play installer for the korva CLI on Windows.
# Detects the architecture, installs korva.exe and adds it to the user PATH.
$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$dist = if ($env:KORVA_DIST) { $env:KORVA_DIST } else { Join-Path $scriptDir "dist" }
$prefix = if ($env:KORVA_PREFIX) { $env:KORVA_PREFIX } else { Join-Path $env:LOCALAPPDATA "Korva\bin" }

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$binary = Join-Path $dist "korva-windows-$arch.exe"
if (-not (Test-Path $binary)) {
    Write-Error "binary not found: $binary (run installer/build.sh first, or set KORVA_DIST)"
    exit 1
}

New-Item -ItemType Directory -Force -Path $prefix | Out-Null
Copy-Item $binary (Join-Path $prefix "korva.exe") -Force
Write-Host "Installed korva to $prefix\korva.exe"

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$prefix*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$prefix", "User")
    Write-Host "Added $prefix to your user PATH — open a new terminal to pick it up."
}

Write-Host ""
Write-Host "Next steps:"
Write-Host "  korva login    # authorize this machine"
Write-Host "  korva setup    # wire VS Code, Claude Code, Cursor, Windsurf to Korva"
Write-Host "  korva status   # see which editors were detected"
