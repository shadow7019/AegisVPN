import SwiftUI

// MARK: - Servers

struct ServersView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        ScrollView {
            VStack(spacing: 18) {
                if !model.daemonReachable {
                    offlineCard
                }
                doubleHopCard
                if model.obfuscatedOnly {
                    obfuscatedNote
                }
                if model.nodesLoaded && visibleNodes.isEmpty {
                    emptyCard
                }
                ForEach(grouped, id: \.country) { group in
                    countrySection(group)
                }
            }
            .padding(22)
        }
        .background(Theme.background)
        .navigationTitle("Servers")
        .task { await model.refreshNodes() }
    }

    // MARK: data

    private var visibleNodes: [Node] {
        model.obfuscatedOnly
            ? model.nodes.filter { $0.type == "obfuscated" }
            : model.nodes
    }

    private var grouped: [(country: String, nodes: [Node])] {
        Dictionary(grouping: visibleNodes, by: \.country)
            .map { (country: $0.key, nodes: $0.value.sorted { $0.city < $1.city }) }
            .sorted { $0.country < $1.country }
    }

    // MARK: sections

    private var offlineCard: some View {
        HStack(spacing: 12) {
            Image(systemName: "wifi.slash")
                .foregroundStyle(Theme.danger)
            Text("The daemon is offline — start it or install it from the Dashboard to load the node registry.")
                .foregroundStyle(Theme.textSecondary)
            Spacer()
            Button("Retry") {
                Task {
                    await model.refreshOnce()
                    await model.refreshNodes()
                }
            }
            .buttonStyle(.bordered)
        }
        .card()
    }

    private var obfuscatedNote: some View {
        HStack(spacing: 10) {
            Image(systemName: "eye.slash")
                .foregroundStyle(Theme.warn)
            Text("Obfuscated-only mode is on — showing obfuscated nodes. UDP traffic will be wrapped with the AegisObfs layer.")
                .font(.callout)
                .foregroundStyle(Theme.textSecondary)
            Spacer()
        }
        .card()
    }

    private var emptyCard: some View {
        VStack(spacing: 8) {
            Image(systemName: "server.rack")
                .font(.system(size: 30))
                .foregroundStyle(Theme.textSecondary)
            Text("No nodes available")
                .font(.headline)
                .foregroundStyle(.white)
            Text("The daemon returned an empty registry. Check coordination-server connectivity in the daemon logs.")
                .font(.callout)
                .foregroundStyle(Theme.textSecondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .card()
    }

    /// Entry → relay chain picker. "None" relay = single hop.
    private var doubleHopCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Label("Double VPN chain", systemImage: "arrow.triangle.branch")
                    .font(.headline)
                    .foregroundStyle(.white)
                Spacer()
                Button {
                    Task { await model.connect() }
                } label: {
                    Label("Connect", systemImage: "bolt.fill")
                }
                .buttonStyle(.borderedProminent)
                .tint(Theme.accent)
                .disabled(!model.daemonReachable)
            }
            Text("Traffic is routed entry → relay → internet. Leave the relay on “None” for a single-hop connection; the relay picker offers Double VPN-capable nodes.")
                .font(.caption)
                .foregroundStyle(Theme.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: 14) {
                Picker("Entry", selection: $model.selectedNodeId) {
                    Text("Auto (best available)").tag("")
                    ForEach(model.nodes) { node in
                        Text("\(Format.flagEmoji(for: node.country)) \(node.city) — \(node.id)")
                            .tag(node.id)
                    }
                }
                .frame(maxWidth: .infinity)

                Image(systemName: "arrow.right")
                    .foregroundStyle(Theme.textSecondary)

                Picker("Relay", selection: $model.relayNodeId) {
                    Text("None — single hop").tag("")
                    ForEach(model.nodes.filter { $0.type == "double" || $0.type == "standard" }) { node in
                        Text("\(Format.flagEmoji(for: node.country)) \(node.city)")
                            .tag(node.id)
                    }
                }
                .frame(maxWidth: .infinity)
            }
        }
        .card()
    }

    private func countrySection(_ group: (country: String, nodes: [Node])) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("\(Format.flagEmoji(for: group.country)) \(group.country)")
                .font(.headline)
                .foregroundStyle(.white)
            VStack(spacing: 6) {
                ForEach(group.nodes) { node in
                    NodeRow(node: node, selected: model.selectedNodeId == node.id) {
                        model.selectedNodeId = node.id
                        if model.relayNodeId == node.id { model.relayNodeId = "" }
                    }
                }
            }
        }
        .card()
    }
}

// MARK: - Node row

struct NodeRow: View {
    let node: Node
    let selected: Bool
    let onSelect: () -> Void

    var body: some View {
        Button(action: onSelect) {
            HStack(spacing: 12) {
                Text(Format.flagEmoji(for: node.country))
                    .font(.title2)
                VStack(alignment: .leading, spacing: 2) {
                    Text(node.city)
                        .font(.body.weight(.medium))
                        .foregroundStyle(.white)
                    Text("\(node.id) · \(node.host)")
                        .font(.caption)
                        .foregroundStyle(Theme.textSecondary)
                        .lineLimit(1)
                }
                Spacer()
                ForEach(NodeType.badges(for: node.type), id: \.self) { badge in
                    NodeBadge(text: badge)
                }
                LoadBar(load: node.load)
                    .frame(width: 90)
                Text("\(node.latencyMs ?? 0) ms")
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(Theme.textSecondary)
                    .frame(width: 64, alignment: .trailing)
                Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(selected ? Theme.accent : Theme.textSecondary.opacity(0.5))
            }
            .padding(10)
            .background(
                RoundedRectangle(cornerRadius: 10)
                    .fill(selected ? Theme.accent.opacity(0.10) : Color.white.opacity(0.03))
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}
