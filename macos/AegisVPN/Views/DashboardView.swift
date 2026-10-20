import SwiftUI

// MARK: - Dashboard

struct DashboardView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        ScrollView {
            VStack(spacing: 18) {
                if model.daemonStatus != .running || !model.daemonReachable {
                    DaemonOnboardingCard()
                }
                if let error = model.errorMessage {
                    ErrorBanner(text: error)
                }
                HStack(alignment: .top, spacing: 18) {
                    ConnectCard()
                        .frame(maxWidth: 400)
                    VStack(spacing: 18) {
                        TunnelSummaryCard()
                        StatsGrid()
                        QuickTogglesCard()
                    }
                }
            }
            .padding(22)
        }
        .background(Theme.background)
        .onAppear { model.refreshDaemonStatus() }
    }
}

// MARK: - Connect card (big circular button + status ring)

private struct ConnectCard: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        VStack(spacing: 18) {
            ConnectStateButton(
                state: model.daemonReachable ? model.status.state : "offline",
                action: toggleTunnel
            )
            .frame(width: 220, height: 220)

            VStack(spacing: 4) {
                Text(headline)
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(.white)
                Text(subline)
                    .font(.callout)
                    .foregroundStyle(Theme.textSecondary)
                    .multilineTextAlignment(.center)
            }
            .padding(.bottom, 4)
        }
        .frame(maxWidth: .infinity)
        .card()
    }

    private var headline: String {
        switch model.status.state {
        case "connected": return "You are protected"
        case "connecting": return "Connecting…"
        case "disconnecting": return "Disconnecting…"
        default: return model.daemonReachable ? "Not connected" : "Daemon offline"
        }
    }

    private var subline: String {
        if !model.daemonReachable {
            return "Install or start the AegisVPN daemon to enable the tunnel."
        }
        if model.status.state == "connected", let node = model.status.node {
            let hops = model.status.hops > 1 ? "Double VPN" : "Single hop"
            return "Via \(node) · \(hops)"
        }
        return "WireGuard-backed tunnel · utun via wireguard-go"
    }

    private func toggleTunnel() {
        Task {
            if model.status.state == "connected" {
                await model.disconnect()
            } else {
                await model.connect()
            }
        }
    }
}

/// Big circular connect button with an animated status ring: static gradient
/// ring when idle, spinning arc while connecting/disconnecting, pulse rings
/// when connected.
struct ConnectStateButton: View {
    let state: String
    let action: () -> Void

    private var isConnected: Bool { state == "connected" }
    private var isTransitioning: Bool { state == "connecting" || state == "disconnecting" }
    private var isOffline: Bool { state == "offline" }

    var body: some View {
        Button(action: action) {
            ZStack {
                if isConnected { PulseRings() }
                Circle().fill(Theme.panelRaised)
                StatusRing(active: isConnected, transitioning: isTransitioning)
                VStack(spacing: 10) {
                    Image(systemName: isConnected ? "checkmark" : "power")
                        .font(.system(size: 44, weight: .semibold))
                        .foregroundStyle(iconColor)
                    Text(label)
                        .font(.headline)
                        .foregroundStyle(.white)
                }
            }
        }
        .buttonStyle(.plain)
        .disabled(isTransitioning)
        .help(isConnected ? "Disconnect" : "Connect")
    }

    private var iconColor: Color {
        if isConnected { return Theme.accent }
        if isTransitioning || isOffline { return Theme.textSecondary }
        return .white
    }

    private var label: String {
        switch state {
        case "connected": return "Connected"
        case "connecting": return "Connecting…"
        case "disconnecting": return "Disconnecting…"
        case "offline": return "Daemon Offline"
        default: return "Connect"
        }
    }
}

/// Rotating arc around the connect button. Spins slowly while idle, fast
/// while the tunnel is transitioning.
struct StatusRing: View {
    var active: Bool
    var transitioning: Bool
    @State private var spinning = false

    var body: some View {
        Circle()
            .trim(from: 0.0, to: transitioning ? 0.72 : 0.98)
            .stroke(
                AngularGradient(
                    colors: active
                        ? [Theme.accent, Theme.accent.opacity(0.15), Theme.accent]
                        : [Theme.accent.opacity(0.05), Theme.accent.opacity(0.35)],
                    center: .center
                ),
                style: StrokeStyle(lineWidth: 7, lineCap: .round)
            )
            .rotationEffect(.degrees(spinning ? 360 : 0))
            .animation(
                transitioning
                    ? .linear(duration: 1.1).repeatForever(autoreverses: false)
                    : .linear(duration: 9).repeatForever(autoreverses: false),
                value: spinning
            )
            .onAppear { spinning = true }
            .onChange(of: transitioning) { _ in
                // Restart the animation so the new speed/duration applies.
                spinning = false
                DispatchQueue.main.async { spinning = true }
            }
    }
}

/// Expanding teal pulse rings shown while connected.
struct PulseRings: View {
    @State private var pulsing = false

    var body: some View {
        ZStack {
            ForEach(0..<3, id: \.self) { index in
                Circle()
                    .stroke(Theme.accent.opacity(0.4), lineWidth: 2)
                    .scaleEffect(pulsing ? 1.45 : 0.92)
                    .opacity(pulsing ? 0 : 0.7)
                    .animation(
                        .easeOut(duration: 2.4)
                            .repeatForever(autoreverses: false)
                            .delay(Double(index) * 0.8),
                        value: pulsing
                    )
            }
        }
        .onAppear { pulsing = true }
    }
}

// MARK: - Tunnel summary

private struct TunnelSummaryCard: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Tunnel").font(.headline).foregroundStyle(.white)
            row("State", model.status.state.capitalized)
            row("Node", model.status.node
                ?? (model.selectedNodeId.isEmpty ? "Auto" : model.selectedNodeId))
            row("Local IP", model.status.localIP ?? "—")
            row("Endpoint", model.status.endpoint ?? "—")
            row("Hops", model.status.hops > 1 ? "2 (Double VPN)" : "1")
            if let error = model.status.error, !error.isEmpty {
                row("Engine error", error)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }

    private func row(_ label: String, _ value: String) -> some View {
        HStack(alignment: .top) {
            Text(label)
                .font(.callout)
                .foregroundStyle(Theme.textSecondary)
                .frame(width: 110, alignment: .leading)
            Text(value)
                .font(.callout.monospaced())
                .foregroundStyle(.white)
                .textSelection(.enabled)
            Spacer()
        }
    }
}

// MARK: - Live stats (polled every 2 s by AppModel)

private struct StatsGrid: View {
    @EnvironmentObject private var model: AppModel

    private let columns = [
        GridItem(.flexible()), GridItem(.flexible()),
        GridItem(.flexible()), GridItem(.flexible())
    ]

    var body: some View {
        LazyVGrid(columns: columns, spacing: 12) {
            StatTile(title: "Download", value: Format.bytes(model.status.rxBytes),
                     systemImage: "arrow.down")
            StatTile(title: "Upload", value: Format.bytes(model.status.txBytes),
                     systemImage: "arrow.up")
            StatTile(title: "Latency",
                     value: model.status.latencyMs.map { "\($0) ms" } ?? "—",
                     systemImage: "bolt")
            StatTile(title: "Handshake", value: Format.handshakeAge(model.status.handshakeAt),
                     systemImage: "clock")
        }
        .card()
    }
}

// MARK: - Quick toggles

private struct QuickTogglesCard: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Protection").font(.headline).foregroundStyle(.white)
            Toggle("Kill Switch", isOn: killSwitchBinding)
            Toggle("Threat Protection (DNS ad/tracker blocking)", isOn: threatBinding)
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
}

// MARK: - Daemon onboarding (privileged install step)

struct DaemonOnboardingCard: View {
    @EnvironmentObject private var model: AppModel

    private var bodyText: String {
        switch model.daemonStatus {
        case .running:
            return "The daemon is running but not answering on its control socket yet — give it a moment, or reinstall below. Logs: /Library/Logs/aegisvpn-daemon.log"
        case .installedNotRunning:
            return "The daemon is installed but launchd has not started it (or it crashed). Reinstall to re-bootstrap the service."
        case .notInstalled:
            return "AegisVPN tunnels traffic through a real wireguard-go utun interface owned by a root launchd daemon. Installing copies the engine to /Library/PrivilegedHelperTools and bootstraps net.aegisvpn.daemon — macOS will ask for your administrator password once."
        }
    }

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: "lock.shield")
                .font(.system(size: 30))
                .foregroundStyle(Theme.accent)
            VStack(alignment: .leading, spacing: 4) {
                Text("Privileged daemon required")
                    .font(.headline)
                    .foregroundStyle(.white)
                Text(bodyText)
                    .font(.callout)
                    .foregroundStyle(Theme.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer()
            Button {
                Task { await model.installDaemon() }
            } label: {
                if model.isBusy {
                    ProgressView().controlSize(.small)
                } else {
                    Text("Install Daemon")
                }
            }
            .buttonStyle(.borderedProminent)
            .tint(Theme.accent)
            .disabled(model.isBusy)
        }
        .card()
    }
}
