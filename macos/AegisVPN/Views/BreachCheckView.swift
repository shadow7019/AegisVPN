import SwiftUI

// MARK: - Dark Web Monitor (breach_check)

struct BreachCheckView: View {
    @EnvironmentObject private var model: AppModel
    @State private var email = ""
    @State private var report: BreachReport?
    @State private var isLoading = false
    @State private var message: String?

    var body: some View {
        ScrollView {
            VStack(spacing: 18) {
                headerCard
                if let message {
                    banner(message)
                }
                if let report {
                    resultsCard(report)
                }
            }
            .padding(22)
        }
        .background(Theme.background)
        .navigationTitle("Dark Web Monitor")
    }

    // MARK: header / query

    private var headerCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label("Dark Web Monitor", systemImage: "eye.trianglebadge.exclamationmark")
                .font(.headline)
                .foregroundStyle(.white)
            Text("Checks whether an email address appears in known credential breaches. The lookup runs in the daemon (op: breach_check) against the coordination API — the address is not stored on this Mac, but it does leave your machine, so only check addresses you are comfortable querying.")
                .font(.callout)
                .foregroundStyle(Theme.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
            HStack {
                TextField("you@example.com", text: $email)
                    .textFieldStyle(.roundedBorder)
                    .disabled(isLoading)
                    .onSubmit(check)
                Button {
                    check()
                } label: {
                    if isLoading {
                        ProgressView().controlSize(.small)
                    } else {
                        Text("Check")
                    }
                }
                .buttonStyle(.borderedProminent)
                .tint(Theme.accent)
                .disabled(isLoading || !email.contains("@"))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }

    private func check() {
        isLoading = true
        message = nil
        Task {
            do {
                report = try await model.breachCheck(email: email)
                if (report?.breaches ?? []).isEmpty {
                    message = "Good news — no known breaches found for \(email)."
                }
            } catch {
                message = AppModel.describe(error)
            }
            isLoading = false
        }
    }

    // MARK: results

    private func resultsCard(_ report: BreachReport) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Label("Breaches (\(report.breaches?.count ?? 0))",
                  systemImage: "list.bullet.clipboard")
                .font(.headline)
                .foregroundStyle(.white)
            if let breaches = report.breaches, !breaches.isEmpty {
                Text("Change the password (and enable 2FA) for any affected account.")
                    .font(.caption)
                    .foregroundStyle(Theme.textSecondary)
                ForEach(breaches) { breach in
                    BreachRow(breach: breach)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }

    private func banner(_ text: String) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "info.circle.fill")
                .foregroundStyle(Theme.accent)
            Text(text)
                .font(.callout)
                .foregroundStyle(Theme.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }
}

private struct BreachRow: View {
    let breach: Breach

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack {
                Text(breach.name ?? breach.domain ?? "Unknown breach")
                    .font(.body.weight(.semibold))
                    .foregroundStyle(.white)
                Spacer()
                if let count = breach.pwnCount {
                    Text("\(count) accounts")
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(Theme.textSecondary)
                }
            }
            if let date = breach.breachDate {
                Text("Breached: \(date)")
                    .font(.caption)
                    .foregroundStyle(Theme.textSecondary)
            }
            if let classes = breach.dataClasses, !classes.isEmpty {
                HStack(spacing: 6) {
                    ForEach(classes, id: \.self) { item in
                        Text(item)
                            .font(.caption2)
                            .foregroundStyle(Theme.accent)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Capsule().fill(Theme.accent.opacity(0.14)))
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 10).fill(Color.white.opacity(0.03)))
    }
}
