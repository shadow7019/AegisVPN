import SwiftUI
import AppKit

// MARK: - App entry

@main
struct AegisVPNApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup("AegisVPN") {
            ContentView()
                .environmentObject(model)
                .frame(minWidth: 1040, minHeight: 660)
                .preferredColorScheme(.dark)
        }
        .windowToolbarStyle(.unified)
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        // Force the dark appearance regardless of system setting.
        NSApp.appearance = NSAppearance(named: .darkAqua)
    }
}

// MARK: - AppModel

/// Single source of UI truth: polls the daemon for `Status`, owns the node
/// registry, user settings and the privileged-daemon lifecycle.
@MainActor
final class AppModel: ObservableObject {
    // Live engine state.
    @Published var status: Status = .disconnected
    @Published var daemonReachable = false
    @Published var activeSocketPath: String?
    @Published var nodes: [Node] = []
    @Published var nodesLoaded = false
    @Published var errorMessage: String?
    @Published var daemonStatus: DaemonStatus = .notInstalled
    @Published var isBusy = false

    // Server selection. Empty string = auto (last used node).
    @Published var selectedNodeId = ""
    @Published var relayNodeId = ""

    // Settings — persisted to UserDefaults, pushed to the daemon on change.
    @Published var killSwitch: Bool {
        didSet { defaults.set(killSwitch, forKey: "settings.killSwitch") }
    }
    @Published var threatProtection: Bool {
        didSet { defaults.set(threatProtection, forKey: "settings.threatProtection") }
    }
    @Published var autoConnect: Bool {
        didSet { defaults.set(autoConnect, forKey: "settings.autoConnect") }
    }
    @Published var obfuscatedOnly: Bool {
        didSet { defaults.set(obfuscatedOnly, forKey: "settings.obfuscatedOnly") }
    }
    @Published var obfsKey: String {
        didSet { defaults.set(obfsKey, forKey: "settings.obfsKey") }
    }
    @Published var splitExcludeRaw: String {
        didSet { defaults.set(splitExcludeRaw, forKey: "settings.splitExclude") }
    }
    @Published var splitIncludeRaw: String {
        didSet { defaults.set(splitIncludeRaw, forKey: "settings.splitInclude") }
    }
    @Published var lastNodeId: String {
        didSet { defaults.set(lastNodeId, forKey: "settings.lastNodeId") }
    }

    private let defaults = UserDefaults.standard
    private let client = EngineClient.shared
    private var pollTask: Task<Void, Never>?
    private var autoConnectAttempted = false

    init() {
        let d = defaults
        killSwitch = d.object(forKey: "settings.killSwitch") as? Bool ?? true
        threatProtection = d.object(forKey: "settings.threatProtection") as? Bool ?? false
        autoConnect = d.object(forKey: "settings.autoConnect") as? Bool ?? false
        obfuscatedOnly = d.object(forKey: "settings.obfuscatedOnly") as? Bool ?? false
        obfsKey = d.string(forKey: "settings.obfsKey") ?? ""
        splitExcludeRaw = d.string(forKey: "settings.splitExclude") ?? ""
        splitIncludeRaw = d.string(forKey: "settings.splitInclude") ?? ""
        lastNodeId = d.string(forKey: "settings.lastNodeId") ?? ""
        daemonStatus = DaemonInstaller.currentStatus()
        startPolling()
    }

    // MARK: polling

    private func startPolling() {
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                await self?.refreshOnce()
                try? await Task.sleep(nanoseconds: 2_000_000_000)
            }
        }
    }

    func refreshOnce() async {
        do {
            status = try await client.status()
            daemonReachable = true
            activeSocketPath = await client.currentSocketPath()
            maybeAutoConnect()
        } catch {
            daemonReachable = false
        }
    }

    private func maybeAutoConnect() {
        guard autoConnect, !autoConnectAttempted, status.state == "disconnected" else { return }
        autoConnectAttempted = true
        Task { await connect() }
    }

    // MARK: tunnel control

    func connect() async {
        errorMessage = nil
        let entry = !selectedNodeId.isEmpty
            ? selectedNodeId
            : (lastNodeId.isEmpty ? nil : lastNodeId)
        let relay = relayNodeId.isEmpty ? nil : relayNodeId
        let opts = ConnectOpts(
            profile: "",
            node: entry,
            relay: relay,
            killSwitch: killSwitch,
            threatProtection: threatProtection,
            splitExclude: splitExclude.isEmpty ? nil : splitExclude,
            splitInclude: splitInclude.isEmpty ? nil : splitInclude,
            obfuscate: obfuscatedOnly,
            obfsKey: obfsKey.isEmpty ? nil : obfsKey,
            autoConnect: autoConnect
        )
        do {
            status = try await client.connect(opts)
            if let entry { lastNodeId = entry }
        } catch {
            errorMessage = Self.describe(error)
        }
    }

    func disconnect() async {
        errorMessage = nil
        do {
            status = try await client.disconnect()
        } catch {
            errorMessage = Self.describe(error)
        }
    }

    // MARK: settings push

    func pushKillSwitch() async {
        do { try await client.setKillSwitch(killSwitch) }
        catch { errorMessage = Self.describe(error) }
    }

    func pushThreatProtection() async {
        do { try await client.setThreatProtection(threatProtection) }
        catch { errorMessage = Self.describe(error) }
    }

    func pushSplitTunnel() async {
        do { try await client.setSplitTunnel(exclude: splitExclude, include: splitInclude) }
        catch { errorMessage = Self.describe(error) }
    }

    // MARK: split tunnel lists

    var splitExclude: [String] { Self.cidrList(fromRaw: splitExcludeRaw) }
    var splitInclude: [String] { Self.cidrList(fromRaw: splitIncludeRaw) }

    static func cidrList(fromRaw raw: String) -> [String] {
        raw
            .split(whereSeparator: \.isNewline)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }

    static func isValidCIDR(_ value: String) -> Bool {
        let parts = value.split(separator: "/").map(String.init)
        guard parts.count == 2, !parts[0].isEmpty, let prefix = Int(parts[1]) else { return false }
        if parts[0].contains(":") { return (0...128).contains(prefix) } // IPv6
        let octets = parts[0].split(separator: ".").map(String.init)
        guard octets.count == 4,
              octets.allSatisfy({ (0...255).contains(Int($0) ?? -1) }) else { return false }
        return (0...32).contains(prefix)
    }

    @discardableResult
    func addSplitExclude(_ cidr: String) -> String? {
        guard Self.isValidCIDR(cidr) else { return "Invalid CIDR: \(cidr)" }
        var list = splitExclude
        guard !list.contains(cidr) else { return nil }
        list.append(cidr)
        splitExcludeRaw = list.joined(separator: "\n")
        return nil
    }

    func removeSplitExclude(_ cidr: String) {
        splitExcludeRaw = splitExclude.filter { $0 != cidr }.joined(separator: "\n")
    }

    @discardableResult
    func addSplitInclude(_ cidr: String) -> String? {
        guard Self.isValidCIDR(cidr) else { return "Invalid CIDR: \(cidr)" }
        var list = splitInclude
        guard !list.contains(cidr) else { return nil }
        list.append(cidr)
        splitIncludeRaw = list.joined(separator: "\n")
        return nil
    }

    func removeSplitInclude(_ cidr: String) {
        splitIncludeRaw = splitInclude.filter { $0 != cidr }.joined(separator: "\n")
    }

    // MARK: nodes

    func refreshNodes() async {
        do {
            nodes = try await client.nodes()
            nodesLoaded = true
        } catch {
            nodesLoaded = true
            errorMessage = Self.describe(error)
        }
    }

    // MARK: daemon lifecycle

    func refreshDaemonStatus() {
        daemonStatus = DaemonInstaller.currentStatus()
    }

    func installDaemon() async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await DaemonInstaller.install()
            refreshDaemonStatus()
            await refreshOnce()
        } catch {
            errorMessage = Self.describe(error)
        }
    }

    func uninstallDaemon() async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await DaemonInstaller.uninstall()
            refreshDaemonStatus()
        } catch {
            errorMessage = Self.describe(error)
        }
    }

    // MARK: dark web monitor

    func breachCheck(email: String) async throws -> BreachReport {
        try await client.breachCheck(email: email)
    }

    // MARK: helpers

    static func describe(_ error: Error) -> String {
        if let engineError = error as? EngineError {
            return engineError.errorDescription ?? String(describing: engineError)
        }
        return error.localizedDescription
    }
}
