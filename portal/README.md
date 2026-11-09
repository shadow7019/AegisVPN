# AegisVPN delivery portal

The web download portal users see at the `/` route. It serves the binaries from
`public/downloads/` with hard anti-caching headers so every rebuild is picked up
immediately (the root cause of "the dmg still won't open" reports was a browser
replaying the very first, corrupted build — fixed in v1.2.1 by versioned filenames
plus `Cache-Control: no-store`).

## Run

```bash
bun install        # or npm install
bun run dev        # Next.js 16 App Router, http://localhost:3000
```

## Ship artifacts

Copy the outputs of the build scripts into `portal/public/downloads/`:

| File | Built by |
|---|---|
| `AegisVPN-Setup-x64.exe` | `scripts/build-windows.ps1` |
| `AegisVPN-win-x64-portable.zip` | `scripts/build-windows.ps1` |
| `AegisVPN-1.2.1.dmg` + `AegisVPN.dmg` alias | `scripts/build-macos-dmg-linux.sh` |
| `AegisVPN-macos.zip` | `scripts/build-macos-dmg-linux.sh` |
| `AegisVPN-android.apk` | `scripts/build-android.sh` |
| `install-macos.sh` | `scripts/install-macos.sh` (tracked verbatim) |
| `SHA256SUMS.txt` | regenerate after every build |
| `RELEASES.md` | release notes served alongside the files |

For this repository, canonical copies of the current v1.2.1 artifacts live in
[`releases/v1.2.1/`](../releases/v1.2.1/) — keep both in sync on a release.

## Key source files

* `src/app/page.tsx` — the whole portal UI: platform cards, per-OS security-dialog
  walkthroughs (SmartScreen / Gatekeeper / Play Protect), browser-side SHA-256
  drop-zone verifier, copy-to-clipboard terminal snippets.
* `next.config.ts` — `headers()` block forcing `no-store, must-revalidate` on
  `/downloads/:path*`.
