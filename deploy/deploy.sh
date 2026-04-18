#!/bin/bash
# Deploy kvserve (and the kv CLI) to a remote host. Idempotent:
# works for first deploy and for upgrades with no special flags.
#
# Usage: ./deploy/deploy.sh [HOST]
#
# HOST defaults to limasoul.com. It must already be reachable via
# SSH without a password prompt, and the ssh user must have sudo.
#
# Binaries shipped:
#   kvserve  -- the HTTP daemon (run by systemd, TLS via autocert)
#   kv       -- client CLI (interactive use, symlinked onto PATH)
#
# Layout on the server:
#   /opt/kvserve/kvserve              daemon, invoked by systemd
#   /opt/kvserve/kv                   client CLI
#   /usr/local/bin/kv                 symlink so the CLI is on PATH
#   /opt/kvserve/data/                per-vault storage (kvserve user)
#   /opt/kvserve/data/acme-cache/     Let's Encrypt cache
#
# All binaries are root-owned (755) so the service user cannot
# rewrite them; only /opt/kvserve/data is writable by the service.
#
# What this script does NOT do:
#   - DNS configuration (point HOST's A record at the machine before running).
#   - Firewall rules (open :80 and :443).
#   - Backup. /opt/kvserve/data holds every vault's ciphertext log
#     plus its auth.key; copy it somewhere before destructive ops.

set -euo pipefail

HOST="${1:-limasoul.com}"
SERVICE="deploy/kvserve.service"

BINARIES=(kvserve kv)
CLI_LINKS=(kv)

# Sanity: must be run from the repo root.
if [ ! -f go.mod ] || [ ! -d cmd/kvserve ]; then
    echo "error: run this script from the kv repo root" >&2
    exit 1
fi

echo "[1/3] cross-compile binaries for linux/amd64 ..."
mkdir -p bin
for b in "${BINARIES[@]}"; do
    GOOS=linux GOARCH=amd64 go build \
        -trimpath -ldflags '-s -w' \
        -o "bin/$b-linux-amd64" "./cmd/$b"
    printf "  "
    ls -lh "bin/$b-linux-amd64" | awk '{printf "%-8s %s\n", $5, $9}'
done

echo "[2/3] stage to $HOST:/tmp ..."
for b in "${BINARIES[@]}"; do
    scp -q "bin/$b-linux-amd64" "$HOST:/tmp/$b.new"
done
scp -q "$SERVICE" "$HOST:/tmp/kvserve.service"

echo "[3/3] remote install on $HOST ..."
ssh "$HOST" sudo bash <<REMOTE
set -euo pipefail

BINARIES=(${BINARIES[@]})
CLI_LINKS=(${CLI_LINKS[@]})

# System user. Created once, never deleted by this script.
if ! id kvserve >/dev/null 2>&1; then
    useradd --system --shell /sbin/nologin --home-dir /opt/kvserve kvserve
    echo "  created system user kvserve"
fi

# Directories. /opt/kvserve is root-owned so the service user
# cannot replace its own binary; /opt/kvserve/data is kvserve-owned
# so the service can write vault logs and the ACME cache there.
mkdir -p /opt/kvserve
mkdir -p /opt/kvserve/data/acme-cache
chown root:root /opt/kvserve
chown -R kvserve:kvserve /opt/kvserve/data
chmod 755 /opt/kvserve
chmod 700 /opt/kvserve/data
chmod 700 /opt/kvserve/data/acme-cache

# Binaries: keep the previous daemon copy as kvserve.prev for
# quick rollback; atomically install the new ones.
for b in "\${BINARIES[@]}"; do
    if [ "\$b" = "kvserve" ] && [ -f /opt/kvserve/kvserve ]; then
        mv /opt/kvserve/kvserve /opt/kvserve/kvserve.prev
    fi
    install -m 755 -o root -g root "/tmp/\$b.new" "/opt/kvserve/\$b"
    rm -f "/tmp/\$b.new"
done

# Symlinks for interactive CLI tools.
for b in "\${CLI_LINKS[@]}"; do
    ln -sf "/opt/kvserve/\$b" "/usr/local/bin/\$b"
done

# Service unit.
install -m 644 -o root -g root /tmp/kvserve.service /etc/systemd/system/kvserve.service
rm -f /tmp/kvserve.service
systemctl daemon-reload

# Start fresh or restart in place.
if systemctl is-enabled --quiet kvserve 2>/dev/null; then
    systemctl restart kvserve
    echo "  restarted kvserve"
else
    systemctl enable --now kvserve
    echo "  enabled and started kvserve"
fi

# Short settling delay so the status below reflects steady state.
sleep 2
systemctl status --no-pager kvserve | head -15
REMOTE

echo
echo "Deployed to $HOST."
echo
echo "Installed binaries:"
for b in "${BINARIES[@]}"; do echo "  /opt/kvserve/$b"; done
echo
echo "CLI symlinks (on PATH):"
for b in "${CLI_LINKS[@]}"; do echo "  /usr/local/bin/$b"; done
echo
echo "Next steps:"
echo "  Logs:         ssh $HOST sudo journalctl -u kvserve -f"
echo "  Health check: curl -sSfI https://$HOST/v1/sync -X POST -o /dev/null -w '%{http_code}\n'"
echo "                (expect 400 — no body — which proves TLS is live)"
echo "  Rollback:     ssh $HOST sudo systemctl stop kvserve \\"
echo "                    '&&' sudo mv /opt/kvserve/kvserve.prev /opt/kvserve/kvserve \\"
echo "                    '&&' sudo systemctl start kvserve"
