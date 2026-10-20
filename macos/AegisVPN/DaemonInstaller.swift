import Foundation

// MARK: - Daemon status

enum DaemonStatus: Equatable {
    case notInstalled
    case installedNotRunning
    case running
}

enum InstallError: LocalizedError {
    case resourceMissing(String)
    case userCancelled
    case privilegedScriptFailed(status: Int32, message: String)

    var errorDescription: String? {
        switch self {
        case .resourceMissing(let path):
            return "Required resource not found in app bundle: \(path)"
        case .userCancelled:
            return "Daemon installation cancelled."
        case .privilegedScriptFailed(let status, let message):
            return "Privileged install failed (exit \(status)): \(message)"
        }
    }
}

// MARK: - DaemonInstaller

/// Manages the privileged engine daemon (net.aegisvpn.daemon).
///
/// The daemon is NOT an embedded SMJobBless helper: the engine binary is
/// shipped inside the app bundle (Contents/Resources/aegisvpn) and installed
/// into /Library/PrivilegedHelperTools by `install-daemon.sh`, which the app
/// executes through an `osascript ... with administrator privileges` prompt.
/// See macos/README.md for the trade-offs.
enum DaemonInstaller {
    static let label = "net.aegisvpn.daemon"
    static let helperPath = "/Library/PrivilegedHelperTools/net.aegisvpn.daemon"
    static let plistPath = "/Library/LaunchDaemons/net.aegisvpn.daemon.plist"
    static let socketPath = "/var/run/aegisvpn.sock"

    // MARK: detection

    static func socketExists() -> Bool {
        FileManager.default.fileExists(atPath: socketPath)
    }

    static func helperInstalled() -> Bool {
        FileManager.default.fileExists(atPath: helperPath)
            && FileManager.default.fileExists(atPath: plistPath)
    }

    /// launchd load check for the system domain. Best effort: reading the
    /// system domain may require privileges; the socket/process probes below
    /// are the primary signals.
    static func isLaunchdServiceLoaded() -> Bool {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        process.arguments = ["print", "system/\(label)"]
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        do { try process.run() } catch { return false }
        process.waitUntilExit()
        return process.terminationStatus == 0
    }

    /// pgrep for the installed helper image.
    static func isProcessRunning() -> Bool {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/pgrep")
        process.arguments = ["-f", "PrivilegedHelperTools/\(label)"]
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        do { try process.run() } catch { return false }
        process.waitUntilExit()
        return process.terminationStatus == 0
    }

    static func currentStatus() -> DaemonStatus {
        if socketExists() || isProcessRunning() || isLaunchdServiceLoaded() {
            return .running
        }
        return helperInstalled() ? .installedNotRunning : .notInstalled
    }

    // MARK: privileged install / uninstall

    /// Runs the bundled install-daemon.sh via an administrator prompt.
    /// The script receives the app bundle path so it can locate the embedded
    /// engine binary and launchd plist inside Contents/Resources.
    static func install(bundlePath: String = Bundle.main.bundlePath) async throws {
        try await runPrivileged(scriptName: "install-daemon.sh", arguments: [bundlePath])
    }

    static func uninstall() async throws {
        try await runPrivileged(scriptName: "uninstall-daemon.sh", arguments: [])
    }

    // MARK: plumbing

    private static func runPrivileged(scriptName: String, arguments: [String]) async throws {
        guard let scriptURL = Bundle.main.resourceURL?.appendingPathComponent(scriptName) else {
            throw InstallError.resourceMissing(scriptName)
        }
        for arg in arguments where !FileManager.default.fileExists(atPath: arg) {
            throw InstallError.resourceMissing(arg)
        }

        var shell = "bash \(shellQuote(scriptURL.path))"
        for arg in arguments {
            shell += " \(shellQuote(arg))"
        }
        let prompt = "AegisVPN needs administrator permission to install its privileged daemon."
        let appleScript = "do shell script \(appleScriptQuote(shell)) "
            + "with administrator privileges with prompt \(appleScriptQuote(prompt))"

        try await Task.detached(priority: .userInitiated) {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
            process.arguments = ["-e", appleScript]
            let stdout = Pipe()
            let stderr = Pipe()
            process.standardOutput = stdout
            process.standardError = stderr
            try process.run()
            let outData = stdout.fileHandleForReading.readDataToEndOfFile()
            let errData = stderr.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()

            guard process.terminationStatus == 0 else {
                let errText = String(decoding: errData, as: UTF8.self)
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                let outText = String(decoding: outData, as: UTF8.self)
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                if errText.contains("User canceled") || errText.contains("-128")
                    || outText.contains("User canceled") {
                    throw InstallError.userCancelled
                }
                throw InstallError.privilegedScriptFailed(status: process.terminationStatus,
                                                          message: errText.isEmpty ? outText : errText)
            }
        }.value
    }

    /// POSIX-shell single-quote quoting: ' -> '\''.
    private static func shellQuote(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    /// AppleScript double-quote literal quoting: \ -> \\ and " -> \".
    private static func appleScriptQuote(_ value: String) -> String {
        let escaped = value
            .replacingOccurrences(of: "\\", with: "\\\\")
            .replacingOccurrences(of: "\"", with: "\\\"")
        return "\"" + escaped + "\""
    }
}
