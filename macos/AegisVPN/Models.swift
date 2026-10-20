import Foundation

// MARK: - Engine Status (PROTOCOL.md §1)

/// Mirrors the engine's `aegis.Status` struct (PROTOCOL.md section 1) exactly.
struct Status: Codable, Equatable {
    /// "disconnected" | "connecting" | "connected" | "disconnecting"
    var state: String
    var profile: String?
    var node: String?
    /// Engine JSON key is "localIp".
    var localIP: String?
    var endpoint: String?
    /// 1 = single hop, 2 = Double VPN.
    var hops: Int
    var killSwitch: Bool
    var threatProtection: Bool
    var rxBytes: UInt64
    var txBytes: UInt64
    /// Unix seconds of last handshake (0 = none yet).
    var handshakeAt: Int64
    var latencyMs: Int?
    var error: String?

    enum CodingKeys: String, CodingKey {
        case state, profile, node
        case localIP = "localIp"
        case endpoint, hops
        case killSwitch
        case threatProtection
        case rxBytes, txBytes, handshakeAt
        case latencyMs, error
    }
}

extension Status {
    /// Neutral offline/disconnected value used before the first daemon poll.
    static var disconnected: Status {
        Status(state: "disconnected", profile: nil, node: nil, localIP: nil,
               endpoint: nil, hops: 1, killSwitch: false, threatProtection: false,
               rxBytes: 0, txBytes: 0, handshakeAt: 0, latencyMs: nil, error: nil)
    }
}

// MARK: - Connect options (PROTOCOL.md §1)

/// Mirrors the engine's `aegis.ConnectOpts` exactly.
struct ConnectOpts: Codable, Equatable {
    /// Path to, or inline contents of, a wg-quick profile. Empty when
    /// connecting through the node registry (`node` below).
    var profile: String
    /// Node id from the registry (PROTOCOL.md §4).
    var node: String?
    /// Second hop node (Double VPN).
    var relay: String?
    var killSwitch: Bool
    var threatProtection: Bool
    /// CIDRs excluded from the tunnel.
    var splitExclude: [String]?
    /// CIDRs forced through the tunnel.
    var splitInclude: [String]?
    var obfuscate: Bool
    var obfsKey: String?
    var autoConnect: Bool
}

// MARK: - Node registry (PROTOCOL.md §4)

/// Mirrors the coordination server's node entry from `GET /v1/nodes`.
struct Node: Codable, Identifiable, Hashable {
    let id: String
    let country: String
    let city: String
    let host: String
    let port: Int
    let pubkey: String
    /// "standard" | "p2p" | "double" | "onion" | "obfuscated" | "dedicated"
    let type: String
    /// 0.0 – 1.0
    let load: Double
    /// Tolerant of servers that omit the field.
    let latencyMs: Int?
}

// MARK: - Meshnet (PROTOCOL.md §2 op `meshnet_peers`)

/// Peer entry for Meshnet. PROTOCOL.md lists the op and its `[MeshPeer]`
/// response but does not pin the field names yet — decoding is deliberately
/// tolerant (every field optional, stable synthetic id) until the contract
/// is finalized.
struct MeshPeer: Codable, Identifiable, Equatable {
    let id: String
    let name: String?
    let pubkey: String?
    let address: String?
    let allowedIPs: [String]?
    let online: Bool?

    init(id: String? = nil, name: String? = nil, pubkey: String? = nil,
         address: String? = nil, allowedIPs: [String]? = nil, online: Bool? = nil) {
        self.id = id ?? pubkey ?? name ?? UUID().uuidString
        self.name = name
        self.pubkey = pubkey
        self.address = address
        self.allowedIPs = allowedIPs
        self.online = online
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try c.decodeIfPresent(String.self, forKey: .id),
            name: try c.decodeIfPresent(String.self, forKey: .name),
            pubkey: try c.decodeIfPresent(String.self, forKey: .pubkey),
            address: try c.decodeIfPresent(String.self, forKey: .address),
            allowedIPs: try c.decodeIfPresent([String].self, forKey: .allowedIPs),
            online: try c.decodeIfPresent(Bool.self, forKey: .online)
        )
    }

    private enum CodingKeys: String, CodingKey {
        case id, name, pubkey, address, online
        case allowedIPs = "allowedIps"
    }
}

// MARK: - Dark Web Monitor (PROTOCOL.md §2 op `breach_check`)

/// `data` for `breach_check`: `{"breaches": [...]}`.
struct BreachReport: Codable, Equatable {
    let breaches: [Breach]?
}

/// One known credential breach. The coordination API's breach fields are not
/// pinned in PROTOCOL.md, so everything is optional.
struct Breach: Codable, Identifiable, Equatable {
    let id: UUID
    let name: String?
    let domain: String?
    let breachDate: String?
    let addedDate: String?
    let pwnCount: Int?
    let dataClasses: [String]?

    init(from decoder: Decoder) throws {
        id = UUID()
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name)
        domain = try c.decodeIfPresent(String.self, forKey: .domain)
        breachDate = try c.decodeIfPresent(String.self, forKey: .breachDate)
        addedDate = try c.decodeIfPresent(String.self, forKey: .addedDate)
        pwnCount = try c.decodeIfPresent(Int.self, forKey: .pwnCount)
        dataClasses = try c.decodeIfPresent([String].self, forKey: .dataClasses)
    }

    private enum CodingKeys: String, CodingKey {
        case name, domain, breachDate, addedDate, pwnCount, dataClasses
    }
}
