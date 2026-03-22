#!/usr/bin/env bash
# HyCert Agent - Deployment Script (binary, no container)
# Usage: sudo bash /hysp/hycert-agent/deployment/deploy.sh
#
# Prerequisites:
#   - hycert-api running on this host (port 8082)
#   - hyadmin-api running on this host (port 8080)
#   - jq installed (dnf install jq / apt install jq)

set -euo pipefail

APP_DIR="/hysp/hycert-agent"
BIN_SRC="$APP_DIR/bin/hycert-agent-linux-amd64"
BIN_DEST="/usr/local/bin/hycert-agent"
CONFIG_DIR="/etc/hycert"
CONFIG_FILE="$CONFIG_DIR/agent.yaml"
BACKUP_DIR="/var/lib/hycert-agent/backups"

HYADMIN_API="http://127.0.0.1:8080"
HYCERT_API="http://127.0.0.1:8082"
TENANT_CODE="system"

# ─── Helpers ──────────────────────────────────────────────────────────────────

die()  { echo "ERROR: $*" >&2; exit 1; }
info() { echo "  → $*"; }

check_deps() {
    command -v jq    >/dev/null 2>&1 || die "jq not found. Install: dnf install jq"
    command -v curl  >/dev/null 2>&1 || die "curl not found"
}

get_jwt() {
    local user="$1" pass="$2"
    curl -sf "$HYADMIN_API/api/v1/auth/login" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_code\":\"$TENANT_CODE\",\"username\":\"$user\",\"password\":\"$pass\"}" \
        | jq -r '.token // empty'
}

# ─── Main ─────────────────────────────────────────────────────────────────────

check_deps

echo "=== [1/6] Install binary ==="
if [ ! -f "$BIN_SRC" ]; then
    die "Binary not found: $BIN_SRC\n  Run 'make build-linux' on dev machine first, then git pull."
fi
cp "$BIN_SRC" "$BIN_DEST"
chmod +x "$BIN_DEST"
info "Installed: $BIN_DEST"
hycert-agent version

echo ""
echo "=== [2/6] Create directories ==="
mkdir -p "$CONFIG_DIR" "$BACKUP_DIR"
info "$CONFIG_DIR"
info "$BACKUP_DIR"

echo ""
echo "=== [3/6] Login to hyadmin-api ==="
read -rp "  Admin username [admin]: " ADMIN_USER
ADMIN_USER="${ADMIN_USER:-admin}"
read -rsp "  Admin password: " ADMIN_PASS
echo ""

JWT=$(get_jwt "$ADMIN_USER" "$ADMIN_PASS")
[ -n "$JWT" ] || die "Login failed. Check credentials."
info "Login OK"

echo ""
echo "=== [4/6] Create Agent Token ==="
if [ -f "$CONFIG_FILE" ]; then
    EXISTING_TOKEN=$(grep -oP 'token:\s*"\K[^"]+' "$CONFIG_FILE" 2>/dev/null || true)
    if [ -n "$EXISTING_TOKEN" ] && [ "$EXISTING_TOKEN" != "hycert_agt_xxxxx..." ]; then
        info "Config already has a token, skipping token creation."
        AGENT_TOKEN="$EXISTING_TOKEN"
    fi
fi

if [ -z "${AGENT_TOKEN:-}" ]; then
    HOSTNAME_VAL=$(hostname)
    info "Creating token for host: $HOSTNAME_VAL"

    RESP=$(curl -sf "$HYCERT_API/api/v1/adm/cert/agent-tokens" \
        -H "Authorization: Bearer $JWT" \
        -H "X-Tenant-ID: $TENANT_CODE" \
        -H "Content-Type: application/json" \
        -d "{\"name\":\"agent-$HOSTNAME_VAL\",\"allowed_hosts\":[\"$HOSTNAME_VAL\"]}")

    AGENT_TOKEN=$(echo "$RESP" | jq -r '.data.token // empty')
    [ -n "$AGENT_TOKEN" ] || die "Failed to create agent token. Response: $RESP"
    info "Token created: ${AGENT_TOKEN:0:20}..."
    echo ""
    echo "  !! 此 token 只顯示一次，已自動寫入 config !!"
    echo ""
fi

echo "=== [5/6] Write config ==="
HOSTNAME_VAL=$(hostname)
cat > "$CONFIG_FILE" << EOF
server:
  url: "$HYCERT_API"
  token: "$AGENT_TOKEN"
  insecure_skip_verify: false

agent:
  hostname: "$HOSTNAME_VAL"
  interval: 3600
  backup: true
  backup_dir: "$BACKUP_DIR"

log:
  level: "debug"
  file: "/var/log/hycert-agent.log"
EOF
chmod 600 "$CONFIG_FILE"
info "Config: $CONFIG_FILE"

echo ""
echo "=== [6/6] Check deployments ==="
DEPS=$(curl -sf "$HYCERT_API/api/v1/agent/cert/deployments?host=$HOSTNAME_VAL" \
    -H "Authorization: Bearer $AGENT_TOKEN" 2>/dev/null || echo '{"success":false}')

DEP_COUNT=$(echo "$DEPS" | jq '.data | length // 0' 2>/dev/null || echo 0)
info "Found $DEP_COUNT deployment(s) for host $HOSTNAME_VAL"

if [ "$DEP_COUNT" -eq 0 ]; then
    echo ""
    echo "  !! 尚無 deployment 資料 !!"
    echo "  !! 請先在 hycert UI 建立 deployment（target_host=$HOSTNAME_VAL, target_service=nginx）!!"
    echo "  !! 建立後再執行: hycert-agent run --config $CONFIG_FILE !!"
    echo ""
else
    echo "$DEPS" | jq -r '.data[] | "  [\(.id)] cert=\(.certificate_id) service=\(.target_service) status=\(.deploy_status) fingerprint=\(.cert_fingerprint[:16])..."'
    echo ""
    read -rp "  立即執行部署？[Y/n]: " RUN_NOW
    RUN_NOW="${RUN_NOW:-Y}"
    if [[ "$RUN_NOW" =~ ^[Yy] ]]; then
        echo ""
        echo "--- Running hycert-agent ---"
        hycert-agent run --config "$CONFIG_FILE"
        echo ""
        echo "--- Run again to verify idempotency ---"
        hycert-agent run --config "$CONFIG_FILE"
    fi
fi

echo ""
echo "Done."
echo "  Binary:  $BIN_DEST"
echo "  Config:  $CONFIG_FILE"
echo "  Log:     /var/log/hycert-agent.log"
echo "  Backup:  $BACKUP_DIR"
echo ""
echo "Usage:"
echo "  hycert-agent run --config $CONFIG_FILE       # 單次執行"
echo "  hycert-agent daemon --config $CONFIG_FILE     # 持續輪詢"
