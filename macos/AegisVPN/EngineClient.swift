import Foundation
import Network

// MARK: - Ops (PROTOCOL.md §2)

enum EngineOp: String {
    case status
    case connect
    case disconnect
    case killswitch
    case threatProtection = "threat_protection"
    case splitTunnel = "split_tunnel"
    case nodes
    case dns
    case breachCheck = "breach_check"
    case meshnetPeers = "meshnet_peers"
}

// MARK: - Errors

enum EngineError: Error, LocalizedError {
    case daemonUnreachable(paths: [String])
    case connectionClosed
    case requestFailed(String)
    case badResponse(String)

    var errorDescription: String? {
        switch self {
        case .daemonUnreachable(let paths):
            return "AegisVPN daemon unreachable (tried \(paths.joined(separator: ", "))). "
                 + "Install or start the privileged daemon from the Dashboard."
        case .connectionClosed:
            return "Connection to the AegisVPN daemon was closed."
        case .requestFailed(let message):
            return message
        case .badResponse(let detail):
            return "Unexpected daemon response: \(detail)"
        }
    }
}

// MARK: - Request/response wire types (PROTOCOL.md §2)
//
// Request:  {"op": string, "args": object}
// Response: {"ok": bool, "data": object, "error": string}
// Framing: one JSON object per line (newline-delimited).

struct EmptyArgs: Encodable {}
struct EmptyData: Decodable {}
struct EnabledArgs: Encodable { let enabled: Bool }
struct SplitTunnelArgs: Encodable { let exclude: [String]; let include: [String] }
struct BreachCheckArgs: Encodable { let email: String }
struct DNSServersArgs: Encodable { let servers: [String] }

struct EngineRequest<Args: Encodable>: Encodable {
    let op: String
    let args: Args
}

struct EngineEnvelope<Payload: Decodable>: Decodable {
    let ok: Bool
    let data: Payload?
    let error: String?
}

// MARK: - AsyncLock

/// Minimal async mutual-exclusion lock. `EngineClient` is an actor, but actors
/// are reentrant across suspension points; this lock keeps exactly one
/// request/response exchange on the wire at a time so newline-framed JSON
/// never interleaves. Only ever touched from the actor's context.
private final class AsyncLock {
    private var locked = false
    private var waiters: [CheckedContinuation<Void, Never>] = []

    func acquire() async {
        if !locked {
            locked = true
            return
        }
        await withCheckedContinuation { waiters.append($0) }
    }

    func release() {
        if waiters.isEmpty {
            locked = false
        } else {
            waiters.removeFirst().resume()
        }
    }
}

// MARK: - EngineClient

/// Local control API client for the AegisVPN daemon (PROTOCOL.md §2).
///
/// The daemon runs as root (launchd) and serves newline-delimited JSON over a
/// unix domain socket. Socket resolution order:
///   1. /var/run/aegisvpn.sock                       (root daemon, preferred)
///   2. /var/run/aegisvpn/engine.sock                ($appRoot convention, §2)
///   3. <App Group group.net.aegisvpn>/engine.sock   (sandbox-friendly)
///   4. ~/Library/Application Support/AegisVPN/engine.sock (user-mode engine)
actor EngineClient {
    static let shared = EngineClient()
    static let appGroupID = "group.net.aegisvpn"
    private static let connectTimeoutSeconds: TimeInterval = 2.5

    private var connection: NWConnection?
    private var readBuffer: [UInt8] = []
    private var activeSocketPath: String?
    private let wireLock = AsyncLock()

    /// Socket path currently in use, if connected (surfaced in Settings).
    func currentSocketPath() -> String? { activeSocketPath }

    // MARK: socket candidates

    static func socketCandidates() -> [String] {
        var paths: [String] = [
            "/var/run/aegisvpn.sock",        // root daemon (preferred)
            "/var/run/aegisvpn/engine.sock"  // app-root convention (PROTOCOL.md §2)
        ]
        let fm = FileManager.default
        if let group = fm.containerURL(forSecurityApplicationGroupIdentifier: appGroupID) {
            paths.append(group.appendingPathComponent("engine.sock").path)
        }
        if let appSupport = fm.urls(for: .applicationSupportDirectory, in: .userDomainMask).first {
            paths.append(appSupport
                .appendingPathComponent("AegisVPN", isDirectory: true)
                .appendingPathComponent("engine.sock").path)
        }
        return paths
    }

    // MARK: ops

    func status() async throws -> Status {
        try await request(op: .status, args: EmptyArgs()) as Status
    }

    func connect(_ opts: ConnectOpts) async throws -> Status {
        try await request(op: .connect, args: opts) as Status
    }

    func disconnect() async throws -> Status {
        try await request(op: .disconnect, args: EmptyArgs()) as Status
    }

    func setKillSwitch(_ enabled: Bool) async throws {
        _ = try await request(op: .killswitch, args: EnabledArgs(enabled: enabled)) as EmptyData
    }

    func setThreatProtection(_ enabled: Bool) async throws {
        _ = try await request(op: .threatProtection, args: EnabledArgs(enabled: enabled)) as EmptyData
    }

    func setSplitTunnel(exclude: [String], include: [String]) async throws {
        _ = try await request(op: .splitTunnel,
                              args: SplitTunnelArgs(exclude: exclude, include: include)) as EmptyData
    }

    func setDNSServers(_ servers: [String]) async throws {
        _ = try await request(op: .dns, args: DNSServersArgs(servers: servers)) as EmptyData
    }

    func nodes() async throws -> [Node] {
        try await request(op: .nodes, args: EmptyArgs()) as [Node]
    }

    func breachCheck(email: String) async throws -> BreachReport {
        try await request(op: .breachCheck, args: BreachCheckArgs(email: email)) as BreachReport
    }

    func meshPeers() async throws -> [MeshPeer] {
        try await request(op: .meshnetPeers, args: EmptyArgs()) as [MeshPeer]
    }

    // MARK: request/response core

    /// Sends `{"op": ..., "args": ...}` as one JSON line and decodes the
    /// `{"ok", "data", "error"}` envelope. Serialized through `wireLock`.
    func request<Args: Encodable, Payload: Decodable>(op: EngineOp,
                                                      args: Args) async throws -> Payload {
        await wireLock.acquire()
        defer { wireLock.release() }

        try await ensureConnected()

        let body: Data
        do {
            body = try JSONEncoder().encode(EngineRequest(op: op.rawValue, args: args))
        } catch {
            throw EngineError.badResponse("could not encode \(op.rawValue) request: \(error)")
        }

        try await send(line: String(decoding: body, as: UTF8.self))

        let reply = try await readLine()
        guard !reply.isEmpty else {
            throw EngineError.badResponse("empty reply for \(op.rawValue)")
        }
        guard let replyData = reply.data(using: .utf8) else {
            throw EngineError.badResponse("non-UTF8 reply for \(op.rawValue)")
        }

        let envelope: EngineEnvelope<Payload>
        do {
            envelope = try JSONDecoder().decode(EngineEnvelope<Payload>.self, from: replyData)
        } catch {
            throw EngineError.badResponse("could not decode \(op.rawValue) reply: \(error)")
        }

        guard envelope.ok else {
            throw EngineError.requestFailed(envelope.error
                ?? "daemon returned ok=false without an error message")
        }
        guard let payload = envelope.data else {
            throw EngineError.badResponse("reply for \(op.rawValue) is missing data")
        }
        return payload
    }

    // MARK: transport

    private func ensureConnected() async throws {
        if let conn = connection, case .ready = conn.state { return }
        connection?.cancel()
        connection = nil
        readBuffer.removeAll()

        let candidates = Self.socketCandidates()
        var lastError: Error = EngineError.daemonUnreachable(paths: candidates)
        for path in candidates {
            do {
                connection = try await Self.connect(path: path)
                activeSocketPath = path
                readBuffer.removeAll()
                return
            } catch {
                lastError = error
            }
        }
        throw lastError
    }

    /// Connects to a unix domain socket with a short deadline. Polling the
    /// state machine keeps this deterministic (no continuation races).
    private static func connect(path: String) async throws -> NWConnection {
        let connection = NWConnection(to: NWEndpoint.Unix(path: path), using: NWParameters())
        connection.start(queue: DispatchQueue(label: "net.aegisvpn.engine-client"))
        let deadline = Date().addingTimeInterval(connectTimeoutSeconds)
        while Date() < deadline {
            switch connection.state {
            case .ready:
                return connection
            case .failed(let error):
                connection.cancel()
                throw error
            case .cancelled:
                throw EngineError.connectionClosed
            default:
                break
            }
            try await Task.sleep(nanoseconds: 25_000_000)
        }
        connection.cancel()
        throw EngineError.daemonUnreachable(paths: [path])
    }

    private func send(line: String) async throws {
        guard let conn = connection else { throw EngineError.connectionClosed }
        var data = Data(line.utf8)
        data.append(0x0A)
        try await withCheckedThrowingContinuation { (cont: CheckedContinuation<Void, Error>) in
            conn.send(content: data, completion: .contentProcessed { error in
                if let error {
                    cont.resume(throwing: error)
                } else {
                    cont.resume()
                }
            })
        }
    }

    private func receiveChunk() async throws -> Data {
        guard let conn = connection else { throw EngineError.connectionClosed }
        return try await withCheckedThrowingContinuation { (cont: CheckedContinuation<Data, Error>) in
            conn.receive(minimumIncompleteLength: 1, maximumLength: 65536) { data, _, isComplete, error in
                if let error {
                    cont.resume(throwing: error)
                } else if let data, !data.isEmpty {
                    cont.resume(returning: data)
                } else if isComplete {
                    cont.resume(throwing: EngineError.connectionClosed)
                } else {
                    cont.resume(returning: Data())
                }
            }
        }
    }

    /// Reads one newline-terminated JSON line, buffering partial chunks.
    private func readLine() async throws -> String {
        while true {
            if let idx = readBuffer.firstIndex(of: 0x0A) {
                let line = String(decoding: readBuffer[0..<idx], as: UTF8.self)
                readBuffer.removeSubrange(0...idx)
                return line
            }
            let chunk = try await receiveChunk()
            if chunk.isEmpty { throw EngineError.connectionClosed }
            readBuffer.append(contentsOf: chunk)
        }
    }
}
