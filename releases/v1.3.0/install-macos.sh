#!/usr/bin/env bash
#
# AegisVPN — one-line macOS installer
#
#   curl -fsSL <origin>/downloads/install-macos.sh | bash -s -- "<origin>"
#
# Why an installer instead of a plain DMG download?
#   Files fetched by curl/wget do NOT carry the com.apple.quarantine
#   attribute (only browser downloads do), so the installed app passes
#   Gatekeeper with ZERO dialogs — the same trick Tailscale / Homebrew
#   use for shipping non-notarized Mac software.
#
set -euo pipefail

# Base URL for artifacts. The download page passes its own origin as $1;
# the fallback is the GitHub release bucket for manual runs.
BASE_URL="${1:-${AEGISVPN_BASE:-https://github.com/shadow7019/AegisVPN/releases/latest/download}}"
APP_DEST="/Applications/AegisVPN.app"
ARTIFACT="AegisVPN-macos.zip"

say()  { printf '\n\033[1;35m▸ %s\033[0m\n' "$*"; }
ok()   { printf '\033[1;32m✔ %s\033[0m\n' "$*"; }
fail() { printf '\n\033[1;31m✘ ERROR: %s\033[0m\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = "Darwin" ] || fail "this installer is for macOS (you are on $(uname -s))"
command -v curl >/dev/null || fail "curl is required"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

say "Downloading AegisVPN (universal: Apple Silicon + Intel)…"
curl -fL --progress-bar "$BASE_URL/$ARTIFACT" -o "$TMP/$ARTIFACT" \
  || fail "download failed — check the URL and your connection"

say "Verifying SHA-256…"
EXPECTED=$(curl -fsSL "$BASE_URL/SHA256SUMS.txt" 2>/dev/null | awk -v f="$ARTIFACT" '$2==f {print $1}' || true)
if [ -n "$EXPECTED" ]; then
  echo "$EXPECTED  $TMP/$ARTIFACT" | shasum -a 256 -c - >/dev/null 2>&1 \
    || fail "checksum mismatch — the download is corrupt; simply run the command again"
  ok "checksum verified ($EXPECTED)"
else
  printf '\033[1;33m! checksum source unavailable — continuing without verification\033[0m\n'
fi

say "Installing to /Applications…"
unzip -q -o "$TMP/$ARTIFACT" -d "$TMP/unpacked"
[ -d "$TMP/unpacked/AegisVPN.app" ] || fail "unexpected archive layout"
mkdir -p /Applications
[ -d "$APP_DEST" ] && rm -rf "$APP_DEST"
if ! ditto "$TMP/unpacked/AegisVPN.app" "$APP_DEST" 2>/dev/null; then
  sudo ditto "$TMP/unpacked/AegisVPN.app" "$APP_DEST" \
    || fail "could not write to /Applications"
fi

# curl downloads carry no quarantine, but strip it defensively — this is
# what makes the app open with no Gatekeeper prompt at all.
xattr -dr com.apple.quarantine "$APP_DEST" 2>/dev/null || true
ok "installed: $APP_DEST"

say "Launching AegisVPN…"
say "If macOS asks for your password, approve it once — the VPN tunnel"
say "needs a utun device (root). The dashboard opens in your browser."
open "$APP_DEST" || true

printf '\n\033[1;32mDone. Dashboard: http://127.0.0.1:8080 (localhost only)\033[0m\n'
printf 'CLI:   /Applications/AegisVPN.app/Contents/MacOS/cli/aegisvpn-darwin-$(uname -m)\n'
printf 'Logs:  /var/root/Library/Logs/AegisVPN-server.log\n'
