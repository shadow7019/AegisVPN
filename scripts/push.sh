#!/usr/bin/env bash
# Push AegisVPN to GitHub once SSH auth is available.
#
# The sandbox generates ~/.ssh/id_ed25519 whose PUBLIC key must be added to
# GitHub first (see README section "Pushing to GitHub"):
#
#   ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFLI+RT70Ld8wJKE2dWYw+Tlgh1xoXtE184AQBlpwYkm
#
#   Repo path:  Settings → Deploy keys → Add deploy key → enable "Allow write access"
#   or Account: Settings → SSH and GPG keys → New SSH key
#
# Then run:  ./scripts/push.sh
set -euo pipefail
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

SSH_BIN="${GITSSH:-$HOME/tools/bin/gitssh}"
if command -v ssh >/dev/null 2>&1; then
  export GIT_SSH_COMMAND="ssh"
elif [[ -x "$SSH_BIN" ]]; then
  export GIT_SSH_COMMAND="$SSH_BIN"
else
  echo "no ssh client found" >&2
  exit 1
fi

git push -u origin main
echo "pushed: $(git log --oneline -1)"
