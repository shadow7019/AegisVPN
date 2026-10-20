# Installs the AegisVPN engine as a Windows service.
#
# The service runs `aegisvpn.exe daemon` under LocalSystem so the real
# WireGuard tunnel (WinTun), the kill-switch firewall lockdown and DNS
# takeover survive logoffs and reboots. The WPF GUI talks to the daemon's
# control API on 127.0.0.1:5599 (docs/PROTOCOL.md section 2).
#
#   .\install-service.ps1 [-Binary C:\path\to\aegisvpn.exe]
#
# Must run elevated (the script self-elevates).
[CmdletBinding()]
param(
    [string]$Binary = ""
)

# --- self-elevate -----------------------------------------------------------
$id = [Security.Principal.WindowsIdentity]::GetCurrent()
if (-not (New-Object Security.Principal.WindowsPrincipal($id)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Host "[install] re-launching elevated..." -ForegroundColor Yellow
    Start-Process -FilePath "powershell.exe" -Verb RunAs -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $PSCommandPath, "-Binary", $Binary)
    exit
}

function Info($m)  { Write-Host "[install] $m" -ForegroundColor Green }
function Warn($m)  { Write-Host "[install] $m" -ForegroundColor Yellow }
function Fail($m)  { Write-Host "[install] $m" -ForegroundColor Red; exit 1 }

# --- locate binary -----------------------------------------------------------
if (-not $Binary) {
    $candidates = @(
        (Join-Path $PSScriptRoot "..\dist\aegisvpn-windows-amd64.exe"),
        (Join-Path $PSScriptRoot "..\..\dist\aegisvpn-windows-amd64.exe"),
        "C:\Program Files\AegisVPN\aegisvpn.exe",
        "C:\Program Files\AegisVPN\bin\aegisvpn.exe"
    )
    foreach ($c in $candidates) {
        if (Test-Path $c) { $Binary = (Resolve-Path $c).Path; break }
    }
}
if (-not $Binary -or -not (Test-Path $Binary)) {
    Fail "aegisvpn.exe not found. Build it: cd engine; go build -o ../dist/aegisvpn-windows-amd64.exe ./cmd/aegisvpn — or pass -Binary."
}
$Binary = (Resolve-Path $Binary).Path
Info "engine binary: $Binary (& $Binary version)"

# --- data root ----------------------------------------------------------------
$root = "C:\ProgramData\AegisVPN"
if (-not (Test-Path $root)) { New-Item -ItemType Directory -Path $root | Out-Null }
& icacls $root /inheritance:r /grant "SYSTEM:(OI)(CI)F" /grant "Administrators:(OI)(CI)F" | Out-Null
Info "data root: $root (SYSTEM-only ACL)"

# --- remove any previous installation ------------------------------------------
$existing = Get-Service -Name "AegisVPN" -ErrorAction SilentlyContinue
if ($existing) {
    Warn "existing service found — stopping and re-registering"
    Stop-Service -Name "AegisVPN" -Force -ErrorAction SilentlyContinue
    & sc.exe delete "AegisVPN" | Out-Null
    Start-Sleep -Seconds 1
}

# --- register service ------------------------------------------------------------
# binPath: quoted exe + args; description documents the real tunnel semantics.
$binPath = "`"$Binary`" daemon --root `"$root`""
& sc.exe create "AegisVPN" binPath= $binPath start= "auto" DisplayName= "AegisVPN Engine" | Out-Null
if ($LASTEXITCODE -ne 0) { Fail "sc.exe create failed ($LASTEXITCODE)" }

& sc.exe description "AegisVPN" "AegisVPN real WireGuard engine (WinTun tunnel, kill switch, DNS takeover). Control API on 127.0.0.1:5599." | Out-Null

# Failure recovery: restart on crash, reset counter after 60s.
& sc.exe failure "AegisVPN" reset= 60 actions= restart/5000/restart/10000/restart/30000 | Out-Null

# Delayed start is friendlier at boot (network stack ready); auto-start on demand:
& sc.exe config "AegisVPN" start= "delayed-auto" | Out-Null

Start-Service -Name "AegisVPN"
$svc = Get-Service -Name "AegisVPN"
if ($svc.Status -ne "Running") { Fail "service did not reach Running state (status: $($svc.Status)) — check Windows Event Log" }

# --- wait for control API --------------------------------------------------------
$deadline = (Get-Date).AddSeconds(15)
$alive = $false
while ((Get-Date) -lt $deadline) {
    try {
        $c = New-Object Net.Sockets.TcpClient
        $c.Connect("127.0.0.1", 5599)
        $c.Close(); $alive = $true; break
    } catch { Start-Sleep -Milliseconds 400 }
}

Write-Host ""
Info "AegisVPN service is $($svc.Status)."
if ($alive) { Info "control API live on 127.0.0.1:5599 — launch the WPF GUI or 'aegisvpn status'." }
else { Warn "control API not reachable yet within 15s — the daemon may still be initializing; check Event Viewer." }
Write-Host ""
Write-Host "  uninstall : .\uninstall-service.ps1" 
Write-Host "  logs      : aegisvpn daemon logs to stdout — view via 'sc.exe interrogation' or run interactively to debug"
