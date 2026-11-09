#!/usr/bin/env bash
# Push AegisVPN to GitHub (git@github.com:shadow7019/AegisVPN.git).
#
# ONE-TIME SETUP — the deploy key below must be registered on GitHub:
#
#   Public key (ed25519, fingerprint SHA256:4lDodY/bGuImTgmrz5ZICwr+GSRLZXOVz9gn0twK1Ak):
#
#     ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINs7dWl6o+rFxKbt0K93rgP11gaL1YJLZd/RMqFUCzJj aegisvpn-deploy-key
#
#   Repo key:  https://github.com/shadow7019/AegisVPN/settings/keys
#              → Add deploy key → paste → enable "Allow write access"
#   Account:   https://github.com/settings/keys → New SSH key (also works)
#
# Then run:  ./scripts/push.sh
set -euo pipefail
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# find an ssh client: system first, then the portable copy under ~/tools
SSH_BIN=""
if command -v ssh >/dev/null 2>&1; then
  SSH_BIN="$(command -v ssh)"
elif [[ -x "$HOME/tools/openssh/usr/bin/ssh" ]]; then
  SSH_BIN="$HOME/tools/openssh/usr/bin/ssh"
else
  echo "no ssh client found (apt-get download openssh-client && dpkg -x)" >&2
  exit 1
fi

export GIT_SSH_COMMAND="$SSH_BIN -i $HOME/.ssh/id_ed25519 -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new"

git push -u origin main
echo "pushed: $(git log --oneline -1)"
