# AegisVPN Windows build script
# Produces gui/desktop/build/bin/AegisVPN.exe + dist/AegisVPN-Setup-x64.exe
#
# Usage:
#   .\scripts\build-windows.ps1 [-SkipGui] [-CertThumbprint <THUMBPRINT>]
param(
    [switch]$SkipGui,
    [string]$CertThumbprint = ""
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot

function Require-Tool($name, $help) {
    if (-not (Get-Command $name -ErrorAction SilentlyContinue)) {
        Write-Error "Missing '$name'. $help"
    }
}

Write-Host "== AegisVPN Windows build ==" -ForegroundColor Cyan

if (-not $SkipGui) {
    Require-Tool "go"     "Install Go 1.24+ from https://go.dev/dl"
    Require-Tool "node"   "Install Node.js 20+ from https://nodejs.org"
    Require-Tool "wails"  "Run: go install github.com/wailsapp/wails/v2/cmd/wails@latest"

    Write-Host "-- building GUI (wails) --"
    Push-Location (Join-Path $Root "gui\desktop")
    try {
        # Sanity-build the engine library first so replace-target errors surface early.
        Push-Location (Join-Path $Root "engine")
        go build ./...
        if ($LASTEXITCODE -ne 0) { throw "engine build failed" }
        Pop-Location

        wails build -platform windows/amd64 -clean
        if ($LASTEXITCODE -ne 0) { throw "wails build failed" }
    } finally {
        Pop-Location
    }
    Write-Host "   gui:  gui\desktop\build\bin\AegisVPN.exe"
}

Write-Host "-- building engine CLI (windows/amd64) --"
Push-Location (Join-Path $Root "engine")
try {
    $env:GOOS = "windows"; $env:GOARCH = "amd64"
    go build -o (Join-Path $Root "dist\aegisvpn-cli.exe") ./cmd/aegisvpn
    if ($LASTEXITCODE -ne 0) { throw "engine cli build failed" }
    Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
} finally {
    Pop-Location
}

Write-Host "-- packaging installer (Inno Setup) --"
$iscc = Get-Command "ISCC.exe" -ErrorAction SilentlyContinue
if (-not $iscc) {
    $candidates = @(
        "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
        "$env:ProgramFiles\Inno Setup 6\ISCC.exe"
    )
    foreach ($c in $candidates) { if (Test-Path $c) { $iscc = $c; break } }
}
if (-not $iscc) {
    Write-Warning "ISCC.exe not found; skipping installer (install Inno Setup 6)."
    exit 0
}

New-Item -ItemType Directory -Force -Path (Join-Path $Root "dist") | Out-Null
& $iscc "/DVERSION=1.0.0" (Join-Path $Root "windows\installer\aegisvpn.iss")
if ($LASTEXITCODE -ne 0) { throw "installer compilation failed" }

if ($CertThumbprint -ne "") {
    $exe = Join-Path $Root "dist\AegisVPN-Setup-x64.exe"
    signtool sign /fd SHA256 /sha1 $CertThumbprint /tr http://timestamp.digicert.com /td SHA256 $exe
    Write-Host "   signed: $exe"
}

Write-Host ""
Write-Host "== DONE ==" -ForegroundColor Green
Write-Host "   installer: dist\AegisVPN-Setup-x64.exe"
Write-Host "   cli:       dist\aegisvpn-cli.exe"
