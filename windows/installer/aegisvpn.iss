; AegisVPN — Inno Setup 6 installer script
; Produces: AegisVPN-Setup-x64.exe
;
; Build:  ISCC.exe /DVERSION=1.0.0 aegisvpn.iss
; Input:  gui/desktop/build/bin/AegisVPN.exe  (wails build output)
;
; Notes:
;  - The engine embeds the WireGuardNT driver via
;    golang.zx2c4.com/wireguard/tun (no separate wintun.dll install needed).
;  - The app requests admin elevation at runtime for route/DNS/firewall
;    takeover; the installer itself requires admin.

#define AppName "AegisVPN"
#define AppPublisher "AegisVPN Project"
#define AppURL "https://github.com/shadow7019/AegisVPN"
#define AppExeName "AegisVPN.exe"

#ifndef VERSION
  #define VERSION "1.0.0"
#endif

[Setup]
AppId={{7A3E9C21-5B84-4D2F-9E1A-AEGISVPN000001}
AppName={#AppName}
AppVersion={#VERSION}
AppVerName={#AppName} {#VERSION}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
LicenseFile=..\..\LICENSE
OutputDir=..\..\dist
OutputBaseFilename=AegisVPN-Setup-x64
SetupIconFile=..\assets\icon.ico
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ArchitecturesInstallIn64BitMode=x64compatible
ArchitecturesAllowed=x64compatible
PrivilegesRequired=admin
MinVersion=10.0
UninstallDisplayIcon={app}\{#AppExeName}

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; \
  GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
Name: "autostart"; Description: "Start {#AppName} when Windows starts"; \
  GroupDescription: "Startup:"; Flags: checkedonce

[Files]
Source: "..\..\gui\desktop\build\bin\AegisVPN.exe"; DestDir: "{app}"; \
  Flags: ignoreversion signonce
Source: "..\..\engine\README-engine.md"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist

[Icons]
Name: "{group}\{#AppName}"; Filename: "{app}\{#AppExeName}"
Name: "{group}\Uninstall {#AppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExeName}"; \
  Tasks: desktopicon

[Registry]
; Autostart (opt-in via task)
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; \
  ValueType: string; ValueName: "AegisVPN"; \
  ValueData: """{app}\{#AppExeName}"" --minimized"; \
  Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\{#AppExeName}"; Description: "{cm:LaunchProgram,{#AppName}}"; \
  Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "taskkill.exe"; Parameters: "/F /IM AegisVPN.exe /T"; \
  Flags: runhidden; RunOnceId: "KillAegis"
Filename: "netsh.exe"; Parameters: "advfirewall firewall delete rule name=""AegisVPN-KS-Block"""; \
  Flags: runhidden; RunOnceId: "DelKSBlock"
Filename: "netsh.exe"; Parameters: "advfirewall firewall delete rule name=""AegisVPN-KS-Allow-Loopback"""; \
  Flags: runhidden; RunOnceId: "DelKSLoop"
Filename: "netsh.exe"; Parameters: "advfirewall firewall delete rule name=""AegisVPN-KS-Allow-Endpoint"""; \
  Flags: runhidden; RunOnceId: "DelKSEp"
Filename: "powershell.exe"; Parameters: "-NoProfile -Command ""Remove-NetFirewallRule -DisplayName 'AegisVPN-KS-Allow-Tunnel' -ErrorAction SilentlyContinue"""; \
  Flags: runhidden; RunOnceId: "DelKSTun"

[UninstallDelete]
; App data (profiles, keychain) intentionally preserved under
; %LOCALAPPDATA%\AegisVPN so upgrades keep credentials.
Type: filesandordirs; Name: "{app}\logs"
