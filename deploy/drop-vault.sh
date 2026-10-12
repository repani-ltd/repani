#!/bin/bash
# Delete one vault from the kvserve on HOST, for good: its entry log
# and auth.key.
#
# Usage: ./deploy/drop-vault.sh [HOST]
#
# HOST defaults to limasoul.com (ssh without a password prompt, sudo),
# as for deploy.sh. The script asks for the vault's pairing or backup
# blob -- for an iou trip or keyring, the link's text after #t or #k --
# without echoing it, or reads it from stdin:
#
#   pbpaste | ./deploy/drop-vault.sh
#
# The blob is the vault's RootKey, so it stays here: `kv vault-id`
# derives the VaultID locally, and only that public id crosses ssh.
# Then, after a confirmation, kvserve is stopped, `kvserve drop-vault`
# removes the vault (it refuses while a server holds it, which is why
# the stop), and kvserve is started again whatever the drop's outcome.
# Downtime is the few seconds that takes; clients retry.
#
# Removal is not revocation: a device still holding the blob re-registers
# the vault and uploads its copy on its next sync. Drop a vault only
# once every device has left it.

set -euo pipefail

HOST="${1:-limasoul.com}"
DATA=/opt/kvserve/data

command -v kv >/dev/null || {
    echo "error: kv not on PATH (GOBIN=~/bin go install ./cmd/kv)" >&2
    exit 1
}

if [ -t 0 ]; then
    read -rsp "pairing or backup blob: " BLOB
    echo
else
    read -r BLOB || true
fi
ID="$(printf '%s' "$BLOB" | kv vault-id -)"
unset BLOB

# Check before stopping anything: a server too old for drop-vault, or
# an id it does not hold, would otherwise cost a restart for nothing.
ssh "$HOST" "/opt/kvserve/kvserve help | grep -q drop-vault" || {
    echo "error: $HOST's kvserve has no drop-vault; deploy first" >&2
    exit 1
}
ssh "$HOST" "sudo test -d $DATA/$ID" || {
    echo "error: no vault $ID on $HOST" >&2
    exit 1
}

read -rp "drop vault $ID from $HOST for good? [y/N] " OK </dev/tty
[ "$OK" = y ] || { echo "not dropped"; exit 1; }

ssh "$HOST" "sudo systemctl stop kvserve
    sudo -u kvserve /opt/kvserve/kvserve drop-vault --data $DATA $ID
    rc=\$?
    sudo systemctl start kvserve
    exit \$rc"
