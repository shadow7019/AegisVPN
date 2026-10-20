# Removes the AegisVPN Windows service.
# The engine handles graceful shutdown: tunnel torn down, kill-switch
# firewall rules and DNS settings reverted.
#
#   .\uninstall-service.ps1 [-PurgeData]
[CmdletBinding()]
param(
    [switch]$PurgeData
)

$id = [Security.Principal.WindowsIdentity]::GetCurrent()
if (-not (New-Object Security.Principal.WindowsPrincipal($id)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Start-Process -FilePath "powershell.exe" -Verb RunAs -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $PSCommandPath, "-PurgeData:$PurgeData")
    exit
}

$svc = Get-Service -Name "AegisVPN" -ErrorAction SilentlyContinue
if (-not $svc) { Write-Host "[uninstall] service not installed — nothing to do" -ForegroundColor Yellow; exit 0 }

if ($svc.Status -eq "Running") {
    # SIGTERM-equivalent: engine disconnects the tunnel and reverts routing/DNS.
    Stop-Service -Name "AegisVPN" -Force
    Write-Host "[uninstall] service stopped (tunnel down, kill-switch rules reverted)" -ForegroundColor Green
}
& sc.exe delete "AegisVPN" | Out-Null
Write-Host "[uninstall] service removed" -ForegroundColor Green

if ($PurgeData) {
    Remove-Item -Recurse -Force "C:\ProgramData\AegisVPN" -ErrorAction SilentlyContinue
    Write-Host "[uninstall] purged C:\ProgramData\AegisVPN (keys, profiles)" -ForegroundColor Green
}
