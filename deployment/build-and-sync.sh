#!/usr/bin/env bash
# Build Linux binary on dev machine and sync to remote host.
# Usage: bash deployment/build-and-sync.sh [HOST]
#   HOST defaults to 10.30.0.70

set -euo pipefail

HOST="${1:-10.30.0.70}"
REMOTE_DIR="/hysp/hycert-agent"

echo "=== Build Linux binary ==="
make build-linux

echo "=== Sync to $HOST ==="
ssh "root@$HOST" "mkdir -p $REMOTE_DIR/bin"
scp bin/hycert-agent-linux-amd64 "root@$HOST:$REMOTE_DIR/bin/"
scp deployment/deploy.sh "root@$HOST:$REMOTE_DIR/deployment/deploy.sh"

echo ""
echo "Done. Now SSH to $HOST and run:"
echo "  sudo bash $REMOTE_DIR/deployment/deploy.sh"
