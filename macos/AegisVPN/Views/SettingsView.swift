import SwiftUI

// MARK: - Settings

struct SettingsView: View {
    @EnvironmentObject private var model: AppModel
    @State private var newExclude = ""
    @State private var newInclude = ""

    var body: some View {
        ScrollView {
            VStack(spacing: 18) {
                protectionCard
                splitTunnelCard
                daemonCard
                aboutCard
            }
            .padding(22)
        }
        .background(Theme.background)
        .navigationTitle("Settings")
    }

    // MARK: protection

    private var protectionCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label("Protection", systemImage: "shield.lefthalf.filled")
                .font(.headline)
                .foregroundStyle(.white)
            Toggle("Kill Switch (pf default-drop anchor)", isOn: killSwitchBinding)
            Toggle("Threat Protection (DNS ad/tracker/malware blocking)", isOn: threatBinding)
            Toggle("Auto-connect on launch", isOn: autoConnectBinding)
            Toggle("Obfuscated servers only", isOn: obfuscatedOnlyBinding)
            if model.obfuscatedOnly {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Obfuscation passphrase (AegisObfsKey)")
                        .font(.caption)
                        .foregroundStyle(Theme.textSecondary)
                    SecureField("passphrase", text: $model.obfsKey)
                        .textFieldStyle(.roundedBorder)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }

    private var killSwitchBinding: Binding<Bool> {
        Binding(
            get: { model.killSwitch },
            set: { newValue in
                model.killSwitch = newValue
                Task { await model.pushKillSwitch() }
            }
        )
    }

    private var threatBinding: Binding<Bool> {
        Binding(
            get: { model.threatProtection },
            set: { newValue in
                model.threatProtection = newValue
                Task { await model.pushThreatProtection() }
            }
        )
    }

    private var autoConnectBinding: Binding<Bool> {
        Binding(
            get: { model.autoConnect },
            set: { model.autoConnect = $0 }
        )
    }

    private var obfuscatedOnlyBinding: Binding<Bool> {
        Binding(
            get: { model.obfuscatedOnly },
            set: { model.obfuscatedOnly = $0 }
        )
    }

    // MARK: split tunneling

    private var splitTunnelCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Label("Split tunneling (route-based CIDRs)", systemImage: "arrow.left.arrow.right.square")
                    .font(.headline)
                    .foregroundStyle(.white)
                Spacer()
                Button("Apply to daemon") {
                    Task { await model.pushSplitTunnel() }
                }
                .buttonStyle(.bordered)
                .tint(Theme.accent)
                .disabled(!model.daemonReachable)
            }
            Text("Excluded CIDRs are routed outside the tunnel (e.g. local LAN); included CIDRs are forced through it. One CIDR per line, applied on connect and via the daemon's split_tunnel op.")
                .font(.caption)
                .foregroundStyle(Theme.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
            HStack(alignment: .top, spacing: 16) {
                CIDREditor(
                    title: "Excluded from tunnel",
                    newEntry: $newExclude,
                    entries: model.splitExclude,
                    onAdd: {
                        if model.addSplitExclude(newExclude.trimmingCharacters(in: .whitespaces)) == nil {
                            newExclude = ""
                            Task { await model.pushSplitTunnel() }
                        }
                    },
                    onRemove: { model.removeSplitExclude($0) }
                )
                CIDREditor(
                    title: "Forced through tunnel",
                    newEntry: $newInclude,
                    entries: model.splitInclude,
                    onAdd: {
                        if model.addSplitInclude(newInclude.trimmingCharacters(in: .whitespaces)) == nil {
                            newInclude = ""
                            Task { await model.pushSplitTunnel() }
                        }
                    },
                    onRemove: { model.removeSplitInclude($0) }
                )
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }

    // MARK: daemon

    private var daemonCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label("Privileged daemon", systemImage: "lock.shield")
                .font(.headline)
                .foregroundStyle(.white)
            LabeledRow("Status", text: daemonStatusText)
            LabeledRow("Helper binary", text: DaemonInstaller.helperPath)
            LabeledRow("launchd plist", text: DaemonInstaller.plistPath)
            LabeledRow("Control socket", text: model.activeSocketPath ?? DaemonInstaller.socketPath)
            HStack {
                Button("Install / Repair…") {
                    Task { await model.installDaemon() }
                }
                .buttonStyle(.borderedProminent)
                .tint(Theme.accent)
                .disabled(model.isBusy)
                Button("Uninstall…", role: .destructive) {
                    Task { await model.uninstallDaemon() }
                }
                .buttonStyle(.bordered)
                .disabled(model.isBusy)
            }
            Text("Install runs install-daemon.sh through an administrator prompt (osascript): it copies the engine to /Library/PrivilegedHelperTools, installs /Library/LaunchDaemons/net.aegisvpn.daemon.plist, bootstraps the service and waits for /var/run/aegisvpn.sock.")
                .font(.caption)
                .foregroundStyle(Theme.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }

    private var daemonStatusText: String {
        switch model.daemonStatus {
        case .running: return "Running (root launchd service net.aegisvpn.daemon)"
        case .installedNotRunning: return "Installed but not running"
        case .notInstalled: return "Not installed"
        }
    }

    // MARK: about

    private var aboutCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("About", systemImage: "info.circle")
                .font(.headline)
                .foregroundStyle(.white)
            LabeledRow("Version",
                       text: Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0.0")
            LabeledRow("Copyright",
                       text: Bundle.main.infoDictionary?["NSHumanReadableCopyright"] as? String
                           ?? "Copyright © 2025 AegisVPN Project")
            Text("AegisVPN runs wireguard-go as a root launchd daemon and drives a real utun interface. This GUI is a control plane only — WireGuard private keys never leave the engine's keychain directory.")
                .font(.caption)
                .foregroundStyle(Theme.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }
}

// MARK: - CIDR list editor

private struct CIDREditor: View {
    let title: String
    @Binding var newEntry: String
    let entries: [String]
    let onAdd: () -> Void
    let onRemove: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.white)
            HStack {
                TextField("192.168.1.0/24", text: $newEntry)
                    .textFieldStyle(.roundedBorder)
                Button("Add", action: onAdd)
                    .disabled(newEntry.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            if entries.isEmpty {
                Text("None")
                    .font(.caption)
                    .foregroundStyle(Theme.textSecondary)
            } else {
                ForEach(entries, id: \.self) { entry in
                    HStack {
                        Text(entry)
                            .font(.caption.monospaced())
                            .foregroundStyle(.white)
                        Spacer()
                        Button {
                            onRemove(entry)
                        } label: {
                            Image(systemName: "minus.circle")
                        }
                        .buttonStyle(.plain)
                        .foregroundStyle(Theme.danger)
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 10).fill(Color.white.opacity(0.03)))
    }
}
