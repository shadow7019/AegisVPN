# Windows build & packaging

Produces `dist/AegisVPN-Setup-x64.exe`.

## Prerequisites (on a Windows 10/11 x64 machine)

1. **Go 1.24+** — https://go.dev/dl
2. **Node.js 20+ / npm** — https://nodejs.org
3. **Wails CLI** — `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
4. **Inno Setup 6** (`ISCC.exe` on PATH or default install) — https://jrsoftware.org/isinfo.php
5. WebView2 Runtime (preinstalled on Win 11; evergreen installer otherwise)

## Build

```powershell
cd AegisVPN
.\scripts\build-windows.ps1            # GUI + installer
.\scripts\build-windows.ps1 -SkipGui   # installer only (already built)
```

Artifacts:

| Artifact | Path |
|---|---|
| App binary | `gui/desktop/build/bin/AegisVPN.exe` |
| Installer  | `dist/AegisVPN-Setup-x64.exe` |

## Engine-only CLI (optional, no GUI)

```powershell
cd engine
$env:GOOS="windows"; $env:GOARCH="amd64"
go build -o ..\dist\aegisvpn-cli.exe ./cmd/aegisvpn
```

The CLI daemon exposes the same control API (`127.0.0.1:5599`) that the GUI
uses, useful for headless servers.

## Signing

The Inno script calls `signonce` — set `signtool` parameters in
`build-windows.ps1` (`-CertThumbprint`) or remove the `sign` flag for
unsigned local builds. SmartScreen reputation requires EV certification.
