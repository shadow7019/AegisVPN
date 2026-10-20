import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var model: AppModel
    @State private var selection: SidebarItem = .dashboard

    enum SidebarItem: String, CaseIterable, Identifiable, Hashable {
        case dashboard
        case servers
        case settings
        case darkWebMonitor

        var id: String { rawValue }

        var title: String {
            switch self {
            case .dashboard: return "Dashboard"
            case .servers: return "Servers"
            case .settings: return "Settings"
            case .darkWebMonitor: return "Dark Web Monitor"
            }
        }

        var systemImage: String {
            switch self {
            case .dashboard: return "gauge"
            case .servers: return "server.rack"
            case .settings: return "gearshape"
            case .darkWebMonitor: return "shield.lefthalf.filled"
            }
        }
    }

    var body: some View {
        NavigationSplitView {
            List(selection: $selection) {
                ForEach(SidebarItem.allCases) { item in
                    Label(item.title, systemImage: item.systemImage)
                        .tag(item)
                }
            }
            .listStyle(.sidebar)
            .safeAreaInset(edge: .bottom) { sidebarFooter }
            .navigationTitle("AegisVPN")
        } detail: {
            detail
        }
        .preferredColorScheme(.dark)
    }

    @ViewBuilder
    private var detail: some View {
        switch selection {
        case .dashboard: DashboardView()
        case .servers: ServersView()
        case .settings: SettingsView()
        case .darkWebMonitor: BreachCheckView()
        }
    }

    private var sidebarFooter: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(model.daemonReachable ? Theme.accent : Theme.danger)
                .frame(width: 8, height: 8)
            Text(model.daemonReachable ? "Daemon online" : "Daemon offline")
                .font(.caption)
                .foregroundStyle(Theme.textSecondary)
            Spacer()
            Text("v\(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0.0")")
                .font(.caption2)
                .foregroundStyle(Theme.textSecondary.opacity(0.7))
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
    }
}
