#!/bin/bash
# Deploy kvserve and the kv CLI to a remote host that already runs
# Caddy as its TLS/reverse-proxy front door. This script ships a
# per-site config block into /etc/caddy/sites/kv.caddy and reloads
# Caddy so kv.limasoul.com goes live.
#
# Prerequisite: Caddy must already be installed and running on HOST.
# That happens on the first run of ../quietcasting-go/deploy/deploy.sh.
#
# Usage: ./deploy/deploy.sh [HOST]
#
# HOST defaults to limasoul.com. It must be reachable via SSH without
# a password prompt and the ssh user must have sudo. DNS for
# kv.limasoul.com must already point at the same machine.
#
# Binaries shipped:
#   kvserve  -- the HTTP daemon (loopback 127.0.0.1:8082)
#   kv       -- client CLI, symlinked onto PATH
#
# Layout on the server:
#   /opt/kvserve/kvserve              daemon (loopback plaintext)
#   /opt/kvserve/kv                   client CLI
#   /usr/local/bin/kv                 symlink so the CLI is on PATH
#   /opt/kvserve/data/                per-vault storage (kvserve user)
#   /opt/kvserve/register.key         16-byte register-gate secret
#   /etc/caddy/sites/kv.caddy         caddy site block (reverse_proxy)
#
# register.key is a 26-char Crockford base32 secret generated on
# first install by `kvserve gen-register-key`. A client must hold the
# same secret (via `kv init/pair --register-key <STRING>` or
# `kv set-register-key <STRING>`); otherwise its REGISTER frame will not
# open under the server's K_reg and is rejected with a BAD_AUTH byte.
# There is no permissionless registration. Read the current secret with:
#   ssh HOST sudo cat /opt/kvserve/register.key
#
# What this script does NOT do:
#   - DNS (kv.limasoul.com must resolve to HOST before running).
#   - Install or configure Caddy — that's qc-go's deploy's job.
#   - Backup /opt/kvserve/data.
#
# NB: a normal deploy PRESERVES /opt/kvserve/data so real vaults survive
# an upgrade. For a one-time reset — e.g. the v1 on-disk format change,
# which the new server cannot load from an older layout — run the deploy
# once with WIPE_DATA=1:
#
#   WIPE_DATA=1 ./deploy/deploy.sh
#
# That removes the vault data (but not register.key); clients then
# re-register on their next sync and re-import their exported state.

set -euo pipefail

HOST="${1:-limasoul.com}"

BINARIES=(kvserve kv)
CLI_LINKS=(kv)

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
scp -q deploy/kvserve.service "$HOST:/tmp/kvserve.service"
scp -q deploy/sites/kv.caddy  "$HOST:/tmp/kv.caddy"

echo "[3/3] remote install on $HOST ..."
ssh "$HOST" sudo bash <<REMOTE
set -euo pipefail

BINARIES=(${BINARIES[@]})
CLI_LINKS=(${CLI_LINKS[@]})
WIPE_DATA=${WIPE_DATA:-0}

# Prerequisite: Caddy is already installed.
if [ ! -x /usr/local/bin/caddy ] || [ ! -d /etc/caddy/sites ]; then
    echo "error: Caddy is not installed on this host. Run" >&2
    echo "       ../quietcasting-go/deploy/deploy.sh $HOST first." >&2
    exit 1
fi

# --- kvserve: user, dirs, binaries, service ----------------------

if ! id kvserve >/dev/null 2>&1; then
    useradd --system --shell /sbin/nologin --home-dir /opt/kvserve kvserve
    echo "  created system user kvserve"
fi

mkdir -p /opt/kvserve
chown root:root /opt/kvserve
chmod 755 /opt/kvserve

# Stop the running daemon so we can atomically swap binaries.
systemctl stop kvserve 2>/dev/null || true

# Install NEW binaries first. register-key generation below requires
# the new 'gen-register-key' subcommand which the old binary lacks.
for b in "\${BINARIES[@]}"; do
    if [ "\$b" = "kvserve" ] && [ -f /opt/kvserve/kvserve ]; then
        mv /opt/kvserve/kvserve /opt/kvserve/kvserve.prev
    fi
    install -m 755 -o root -g root "/tmp/\$b.new" "/opt/kvserve/\$b"
    rm -f "/tmp/\$b.new"
done

# Vault data is preserved across deploys. A one-time reset is opt-in via
# WIPE_DATA=1 — needed for the v1 cutover, since the server cannot load
# an older on-disk format and would otherwise fail to start. register.key
# lives outside data/ and is never touched here.
if [ "\$WIPE_DATA" = "1" ]; then
    echo "  WIPE_DATA=1 — removing existing vault data at /opt/kvserve/data"
    rm -rf /opt/kvserve/data
fi
mkdir -p /opt/kvserve/data
chown -R kvserve:kvserve /opt/kvserve/data
chmod 700 /opt/kvserve/data

# Register-gate secret: Crockford base32 text file, generate once,
# never regenerate (rotating invalidates every device that cached it).
# Use -s (non-empty) rather than -f so a prior failed deploy that
# left a zero-byte file does not prevent regeneration.
if [ ! -s /opt/kvserve/register.key ]; then
    rm -f /opt/kvserve/register.key
    # Create the file 0600 from the outset (umask in a subshell) so the
    # secret is never world-readable, not even in the window before chmod.
    ( umask 077; /opt/kvserve/kvserve gen-register-key > /opt/kvserve/register.key )
    chown kvserve:kvserve /opt/kvserve/register.key
    chmod 600 /opt/kvserve/register.key
    echo "  generated /opt/kvserve/register.key"
fi

for b in "\${CLI_LINKS[@]}"; do
    ln -sf "/opt/kvserve/\$b" "/usr/local/bin/\$b"
done

install -m 644 -o root -g root /tmp/kvserve.service /etc/systemd/system/kvserve.service
rm -f /tmp/kvserve.service

# --- caddy site block ---------------------------------------------

install -m 644 -o root -g root /tmp/kv.caddy /etc/caddy/sites/kv.caddy
rm -f /tmp/kv.caddy

# Validate config before asking Caddy to reload. caddy validate is
# strict and will refuse if any imported site block is malformed.
if ! /usr/local/bin/caddy validate --config /etc/caddy/Caddyfile >/dev/null 2>&1; then
    echo "error: /etc/caddy/Caddyfile failed validation after adding kv.caddy" >&2
    /usr/local/bin/caddy validate --config /etc/caddy/Caddyfile || true
    rm -f /etc/caddy/sites/kv.caddy
    exit 1
fi

systemctl daemon-reload
systemctl reload caddy
echo "  reloaded caddy"

if systemctl is-enabled --quiet kvserve 2>/dev/null; then
    systemctl restart kvserve
    echo "  restarted kvserve"
else
    systemctl enable --now kvserve
    echo "  enabled and started kvserve"
fi

sleep 2
systemctl status --no-pager kvserve | head -10

# Emit the register token so deploy.sh can surface it without a
# second ssh round-trip. This leaks it into the local shell's stdout,
# which the caller captures; nothing persisted on the remote.
echo "-- register-token --"
cat /opt/kvserve/register.key
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
echo "Caddy site block:"
echo "  /etc/caddy/sites/kv.caddy          (kv.limasoul.com → 127.0.0.1:8082)"
echo
echo "Next steps:"
echo "  Logs (kvserve):     ssh $HOST sudo journalctl -u kvserve -f"
echo "  Show register key:  ssh $HOST sudo cat /opt/kvserve/register.key"
echo "  Init a client:      kv init --register-key <BASE32>"
echo "  Rollback:           ssh $HOST sudo systemctl stop kvserve \\"
echo "                          '&&' sudo mv /opt/kvserve/kvserve.prev /opt/kvserve/kvserve \\"
echo "                          '&&' sudo systemctl start kvserve"
