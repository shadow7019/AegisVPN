# AegisVPN — Native Windows GUI (WPF, .NET 8)

A fully native Windows client driving the real engine daemon over its control
API (`docs/PROTOCOL.md` §2): newline-delimited JSON on `127.0.0.1:5599`.

```
┌────────────────────────┐         127.0.0.1:5599         ┌─────────────────────┐
│  AegisVPN.exe (WPF)    │ ── JSON line req/resp ───────▶ │  aegisvpn.exe daemon │
│  MVVM · dark UI        │ ◀───────────────────────────── │  (Windows service or │
│  no external packages  │  status/connect/killswitch/…   │   auto-spawned)      │
└────────────────────────┘                                └─────────────────────┘
```

## Build

```powershell
dotnet publish windows/wpf/AegisVPN.Wpf -c Release -r win-x64 `
    -p:PublishSingleFile=true --self-contained false
# → windows/wpf/AegisVPN.Wpf/bin/Release/net8.0-windows/win-x64/publish/AegisVPN.exe
```

Requires the .NET 8 SDK (Windows). CI builds it on `windows-latest`
(see `.github/workflows/ci.yml`).

## Ship it

1. Put `aegisvpn.exe` (engine build: `engine/` → `go build ./cmd/aegisvpn`)
   next to `AegisVPN.exe`, **or** install the engine service first:
   `windows/service/install-service.ps1`.
2. Optional installer: `windows/installer/aegisvpn.iss` (Inno Setup) bundles
   both binaries into `AegisVPN-Setup.exe`.

The GUI self-elevates (`requireAdministrator`) — TUN creation, routing
ownership and the kill-switch firewall lockdown require admin. The GUI
launches the daemon automatically if the service isn't installed.

## Feature surface (engine ops → UI)

| Control op | UI |
|---|---|
| `status` (1s poll) | dashboard ring, rates, session timer, handshake age, error line |
| `connect` | big button, per-node Connect, Quick connect, profile import, Double VPN relay picker |
| `disconnect` | big button |
| `killswitch` | dashboard quick toggle + Settings |
| `threat_protection` | dashboard quick toggle + Settings |
| `split_tunnel` | Settings split include/exclude lists |
| `dns` | Settings DNS field (applied at connect) |
| `nodes` | Servers tab (grouped by country, load bars, latency, type badges) |
| `breach_check` | Dark Web Monitor tab |
| `meshnet_peers` / `meshnet_register` | Meshnet tab |
| `ping` | status-bar version badge |

Settings persist to `%APPDATA%\AegisVPN\settings.json`; obfuscation key is
masked. Keys never live in the GUI — the engine owns the keychain dir.

## Layout

```
AegisVPN.Wpf/
├── AegisVPN.Wpf.csproj    net8.0-windows · UseWPF · zero NuGet deps
├── app.manifest           requireAdministrator · PerMonitorV2 DPI
├── App.xaml(.cs)          dark theme resource dictionary + crash log
├── Models/VpnModels.cs    protocol models (System.Text.Json, exact keys)
├── Services/
│   ├── EngineClient.cs    TCP JSON-line client, reconnect, daemon auto-spawn
│   └── SettingsStore.cs   %APPDATA% persistence
├── ViewModels/            ViewModelBase, RelayCommand, Main/Servers/Settings/Meshnet/Breach
├── Converters/Converters.cs
└── Views/                 MainWindow (custom chrome) + 5 tab views
```
