import SwiftUI

// MARK: - Theme
//
// Dark navy surfaces (never blue/indigo as accents) with a teal/cyan
// accent — #17C7BD — used for connected state, CTAs and the status ring.

enum Theme {
    /// #17C7BD
    static let accent = Color(red: 23 / 255.0, green: 199 / 255.0, blue: 189 / 255.0)
    /// #070E16 — app background (dark navy)
    static let background = Color(red: 7 / 255.0, green: 14 / 255.0, blue: 22 / 255.0)
    /// #0D1620 — card surface
    static let panel = Color(red: 13 / 255.0, green: 22 / 255.0, blue: 32 / 255.0)
    /// #121D2A — raised surface (connect button interior)
    static let panelRaised = Color(red: 18 / 255.0, green: 29 / 255.0, blue: 42 / 255.0)
    static let textSecondary = Color.white.opacity(0.62)
    static let danger = Color(red: 255 / 255.0, green: 82 / 255.0, blue: 82 / 255.0)
    static let warn = Color(red: 255 / 255.0, green: 184 / 255.0, blue: 51 / 255.0)
}

// MARK: - Status presentation helpers

extension Status {
    var isConnected: Bool { state == "connected" }
    var isTransitioning: Bool { state == "connecting" || state == "disconnecting" }

    var stateColor: Color {
        switch state {
        case "connected": return Theme.accent
        case "connecting", "disconnecting": return Theme.warn
        default: return Theme.textSecondary
        }
    }
}

// MARK: - Formatters

enum Format {
    static func bytes(_ value: UInt64) -> String {
        let units = ["B", "KB", "MB", "GB", "TB"]
        var size = Double(value)
        var unit = 0
        while size >= 1024 && unit < units.count - 1 {
            size /= 1024
            unit += 1
        }
        return unit == 0
            ? String(format: "%.0f %@", size, units[unit])
            : String(format: "%.1f %@", size, units[unit])
    }

    static func handshakeAge(_ unixSeconds: Int64) -> String {
        guard unixSeconds > 0 else { return "—" }
        let age = max(0, Int64(Date().timeIntervalSince1970) - unixSeconds)
        if age < 60 { return "\(age)s ago" }
        if age < 3600 { return "\(age / 60)m ago" }
        if age < 86400 { return "\(age / 3600)h ago" }
        return "\(age / 86400)d ago"
    }

    static func flagEmoji(for countryCode: String) -> String {
        let code = countryCode.uppercased()
        guard code.count == 2 else { return "🌐" }
        let regionalIndicatorA: UInt32 = 0x1F1E6
        var emoji = ""
        for scalar in code.unicodeScalars {
            guard scalar.value >= 65, scalar.value <= 90,
                  let flag = Unicode.Scalar(regionalIndicatorA + (scalar.value - 65)) else {
                return "🌐"
            }
            emoji.unicodeScalars.append(flag)
        }
        return emoji
    }
}

// MARK: - Node type badges (P2P / Double VPN / Onion / Obfuscated / Dedicated)

enum NodeType {
    static func badges(for type: String) -> [String] {
        switch type {
        case "p2p": return ["P2P"]
        case "double": return ["Double VPN"]
        case "onion": return ["Onion over VPN"]
        case "obfuscated": return ["Obfuscated"]
        case "dedicated": return ["Dedicated IP"]
        case "standard": return []
        default: return [type.uppercased()]
        }
    }
}

// MARK: - Reusable views

struct CardBackground: ViewModifier {
    func body(content: Content) -> some View {
        content
            .padding(18)
            .background(
                RoundedRectangle(cornerRadius: 14)
                    .fill(Theme.panel)
                    .overlay(
                        RoundedRectangle(cornerRadius: 14)
                            .strokeBorder(Color.white.opacity(0.06), lineWidth: 1)
                    )
            )
    }
}

extension View {
    func card() -> some View { modifier(CardBackground()) }
}

struct LabeledRow: View {
    let label: String
    let text: String

    init(_ label: String, text: String) {
        self.label = label
        self.text = text
    }

    var body: some View {
        HStack(alignment: .top) {
            Text(label)
                .font(.callout)
                .foregroundStyle(Theme.textSecondary)
                .frame(width: 150, alignment: .leading)
            Text(text)
                .font(.callout.monospaced())
                .foregroundStyle(.white)
                .textSelection(.enabled)
            Spacer()
        }
    }
}

struct StatTile: View {
    let title: String
    let value: String
    let systemImage: String

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Label(title, systemImage: systemImage)
                .font(.caption)
                .foregroundStyle(Theme.textSecondary)
            Text(value)
                .font(.title3.weight(.semibold).monospacedDigit())
                .foregroundStyle(.white)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 10).fill(Color.white.opacity(0.04)))
    }
}

struct NodeBadge: View {
    let text: String

    private var color: Color {
        switch text {
        case "P2P": return Theme.accent
        case "Double VPN": return Theme.warn
        case "Onion over VPN": return Color(red: 1.0, green: 0.45, blue: 0.35)
        case "Obfuscated": return Color(red: 0.40, green: 0.85, blue: 0.75)
        case "Dedicated IP": return Color.white.opacity(0.8)
        default: return Theme.textSecondary
        }
    }

    var body: some View {
        Text(text)
            .font(.caption2.weight(.semibold))
            .foregroundStyle(color)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(Capsule().fill(color.opacity(0.16)))
    }
}

struct LoadBar: View {
    /// 0.0 – 1.0
    let load: Double

    private var loadColor: Color {
        if load < 0.6 { return Theme.accent }
        if load < 0.85 { return Theme.warn }
        return Theme.danger
    }

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(Color.white.opacity(0.08))
                Capsule()
                    .fill(loadColor)
                    .frame(width: geo.size.width * CGFloat(min(max(load, 0), 1)))
            }
        }
        .frame(height: 6)
        .help("Node load: \(Int((load * 100).rounded()))%")
    }
}

struct ErrorBanner: View {
    let text: String
    @EnvironmentObject private var model: AppModel

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(Theme.warn)
            Text(text)
                .font(.callout)
                .foregroundStyle(.white)
                .fixedSize(horizontal: false, vertical: true)
            Spacer()
            Button {
                model.errorMessage = nil
            } label: {
                Image(systemName: "xmark")
            }
            .buttonStyle(.plain)
            .foregroundStyle(Theme.textSecondary)
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 12).fill(Theme.warn.opacity(0.12)))
    }
}
