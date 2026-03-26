#!/usr/bin/env bash
# HyCert Agent - Unified Deployment Script (Linux)
# Usage: sudo bash /hysp/hycert-agent/deployment/deploy.sh
set -euo pipefail

APP_DIR="/hysp/hycert-agent"
BIN_SRC="$APP_DIR/bin/hycert-agent-linux-amd64"
BIN_DEST="/usr/local/bin/hycert-agent"
CONFIG_DIR="/etc/hycert"
CONFIG_FILE="$CONFIG_DIR/agent.yaml"
AGENT_ID_FILE="$CONFIG_DIR/agent-id"
BACKUP_DIR="/var/lib/hycert-agent/backups"
LOG_DIR="/var/log/hycert-agent"
LOG_FILE="$LOG_DIR/agent.log"
SERVICE_NAME="hycert-agent"

# ─── Helpers ──────────────────────────────────────────────────────────────────

die()  { echo "ERROR: $*" >&2; exit 1; }
info() { echo "  → $*"; }

get_jwt() {
    local url="$1" tenant="$2" user="$3" pass="$4"
    curl -sf "$url/api/v1/auth/login" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_code\":\"$tenant\",\"username\":\"$user\",\"password\":\"$pass\"}" \
        | jq -r '.token // empty'
}

derive_hyadmin_url() {
    local hycert_url="$1"
    echo "${hycert_url%/hycert-api*}/hyadmin-api"
}

parse_yaml_value() {
    local file="$1" key="$2"
    grep -oP "${key}:\s*\"?\K[^\"]*" "$file" 2>/dev/null | head -1 || true
}

# ─── [1/8] Check prerequisites ───────────────────────────────────────────────

echo "=== [1/8] Check prerequisites ==="
[ "$(id -u)" -eq 0 ] || die "Please run as root (sudo)"
command -v jq   >/dev/null 2>&1 || die "jq not found. Install: dnf install jq"
command -v curl >/dev/null 2>&1 || die "curl not found"
info "OK"

# ─── [2/8] Install binary ────────────────────────────────────────────────────

echo ""
echo "=== [2/8] Install binary ==="
if systemctl is-active "$SERVICE_NAME" &>/dev/null; then
    info "Stopping running $SERVICE_NAME..."
    $BIN_DEST service stop 2>/dev/null || systemctl stop "$SERVICE_NAME" 2>/dev/null || true
fi
if [ ! -f "$BIN_SRC" ]; then
    die "Binary not found: $BIN_SRC\n  Run 'make build-linux' first."
fi
cp "$BIN_SRC" "$BIN_DEST"
chmod +x "$BIN_DEST"
info "Installed: $BIN_DEST"
$BIN_DEST version

# ─── [3/8] Create directories ────────────────────────────────────────────────

echo ""
echo "=== [3/8] Create directories ==="
mkdir -p "$CONFIG_DIR" "$BACKUP_DIR" "$LOG_DIR"
info "$CONFIG_DIR"
info "$BACKUP_DIR"
info "$LOG_DIR"

# ─── [4/8] Check existing config ─────────────────────────────────────────────

echo ""
echo "=== [4/8] Check existing config ==="
SKIP_SETUP=false

if [ -f "$CONFIG_FILE" ]; then
    EXISTING_URL=$(parse_yaml_value "$CONFIG_FILE" "url")
    EXISTING_TOKEN=$(parse_yaml_value "$CONFIG_FILE" "token")
    EXISTING_NAME=$(parse_yaml_value "$CONFIG_FILE" "name")
    TOKEN_PREFIX="${EXISTING_TOKEN:0:20}"

    echo "  Found existing config:"
    echo "    Server URL:  ${EXISTING_URL:-unknown}"
    echo "    Token:       ${TOKEN_PREFIX}..."
    echo "    Agent Name:  ${EXISTING_NAME:-unknown}"
    echo ""

    # Validate token
    if [ -n "$EXISTING_TOKEN" ] && [ "$EXISTING_TOKEN" != "hycert_agt_xxxxx..." ]; then
        info "Validating token..."
        AGENT_UUID=$(head -1 "$AGENT_ID_FILE" 2>/dev/null || echo "")
        if [ -n "$AGENT_UUID" ]; then
            VALIDATE_RESP=$(curl -sf "${EXISTING_URL}/api/v1/agent/cert/register" \
                -H "Authorization: Bearer $EXISTING_TOKEN" \
                -H "X-Agent-ID: $AGENT_UUID" \
                -H "Content-Type: application/json" \
                -d "{\"agent_id\":\"$AGENT_UUID\",\"hostname\":\"$(hostname)\"}" 2>/dev/null || echo "")
            if echo "$VALIDATE_RESP" | jq -e '.success == true' >/dev/null 2>&1; then
                info "Token is valid ✓"
                echo ""
                read -rp "  [1] Continue with existing settings (default)
  [2] Reconfigure (full setup)
  Choose [1]: " CHOICE
                CHOICE="${CHOICE:-1}"
                if [ "$CHOICE" = "1" ]; then
                    SKIP_SETUP=true
                fi
            else
                info "Token validation failed. Proceeding with full setup."
            fi
        else
            info "No agent-id found. Proceeding with full setup."
        fi
    fi
else
    info "No existing config found. Starting fresh setup."
fi

if [ "$SKIP_SETUP" = true ]; then
    echo ""
    echo "  Skipping to service install..."
else

# ─── [5/8] Collect settings ──────────────────────────────────────────────────

echo ""
echo "=== [5/8] Collect settings ==="

# Server URL
DEFAULT_URL="${EXISTING_URL:-}"
read -rp "  Server URL (e.g., https://domain/hycert-api)${DEFAULT_URL:+ [$DEFAULT_URL]}: " SERVER_URL
SERVER_URL="${SERVER_URL:-$DEFAULT_URL}"
[ -n "$SERVER_URL" ] || die "Server URL is required"

# Proxy
read -rp "  HTTP proxy (empty = none): " PROXY
PROXY="${PROXY:-}"

# SSL verify
read -rp "  Skip SSL verify? (y/N): " SKIP_SSL
if [[ "$SKIP_SSL" =~ ^[Yy] ]]; then
    INSECURE="true"
else
    INSECURE="false"
fi

# Tenant code
read -rp "  Tenant code [system]: " TENANT_CODE
TENANT_CODE="${TENANT_CODE:-system}"

# Admin credentials
read -rp "  Admin username [admin]: " ADMIN_USER
ADMIN_USER="${ADMIN_USER:-admin}"
read -rsp "  Admin password: " ADMIN_PASS
echo ""
[ -n "$ADMIN_PASS" ] || die "Password is required"

# Label
read -rp "  Token label (for grouping, empty = none): " LABEL
LABEL="${LABEL:-}"

# Agent name
DEFAULT_NAME="$(hostname)"
read -rp "  Agent display name [$DEFAULT_NAME]: " AGENT_NAME
AGENT_NAME="${AGENT_NAME:-$DEFAULT_NAME}"

# Interval
read -rp "  Poll interval in seconds [3600]: " INTERVAL
INTERVAL="${INTERVAL:-3600}"

# ─── [6/8] Login and acquire token ───────────────────────────────────────────

echo ""
echo "=== [6/8] Login and acquire token ==="

# Derive hyadmin URL
HYADMIN_URL=$(derive_hyadmin_url "$SERVER_URL")
info "hyadmin URL: $HYADMIN_URL"

# Verify hyadmin connectivity
HEALTH=$(curl -sf "$HYADMIN_URL/api/v1/health" 2>/dev/null || echo "")
if [ -z "$HEALTH" ]; then
    echo "  Warning: Cannot reach $HYADMIN_URL/api/v1/health"
    read -rp "  Enter hyadmin API URL manually: " HYADMIN_URL
    [ -n "$HYADMIN_URL" ] || die "hyadmin URL is required for login"
fi

# Login
JWT=$(get_jwt "$HYADMIN_URL" "$TENANT_CODE" "$ADMIN_USER" "$ADMIN_PASS")
[ -n "$JWT" ] || die "Login failed. Check credentials."
info "Login OK"

# Acquire token
AGENT_TOKEN=""

if [ -n "$LABEL" ]; then
    info "Checking existing token for label: $LABEL"
    LABEL_RESP=$(curl -sf "$SERVER_URL/api/v1/adm/cert/agent-tokens/by-label/$LABEL" \
        -H "Authorization: Bearer $JWT" 2>/dev/null || echo "")

    if echo "$LABEL_RESP" | jq -e '.success == true' >/dev/null 2>&1; then
        AGENT_TOKEN=$(echo "$LABEL_RESP" | jq -r '.data.token // empty')
        if [ -n "$AGENT_TOKEN" ]; then
            TOKEN_PREFIX=$(echo "$LABEL_RESP" | jq -r '.data.token_prefix // empty')
            info "Reusing existing token for label '$LABEL': ${TOKEN_PREFIX}..."
        fi
    fi
fi

if [ -z "$AGENT_TOKEN" ]; then
    info "Creating new token: token-$AGENT_NAME"
    CREATE_BODY="{\"name\":\"token-$AGENT_NAME\""
    if [ -n "$LABEL" ]; then
        CREATE_BODY="$CREATE_BODY,\"label\":\"$LABEL\""
    fi
    CREATE_BODY="$CREATE_BODY}"

    RESP=$(curl -sf "$SERVER_URL/api/v1/adm/cert/agent-tokens" \
        -H "Authorization: Bearer $JWT" \
        -H "Content-Type: application/json" \
        -d "$CREATE_BODY" 2>/dev/null || echo "")

    AGENT_TOKEN=$(echo "$RESP" | jq -r '.data.token // empty')
    [ -n "$AGENT_TOKEN" ] || die "Failed to create token. Response: $RESP"
    info "Token created: ${AGENT_TOKEN:0:20}..."
fi

# ─── [7/8] Write config ──────────────────────────────────────────────────────

echo ""
echo "=== [7/8] Write config ==="
cat > "$CONFIG_FILE" << EOF
server:
  url: "$SERVER_URL"
  token: "$AGENT_TOKEN"
  proxy: "$PROXY"
  insecure_skip_verify: $INSECURE

agent:
  name: "$AGENT_NAME"
  interval: $INTERVAL
  backup: true
  backup_dir: "$BACKUP_DIR"

log:
  level: "debug"
  file: "$LOG_FILE"
  max_size: 10
  max_backups: 3
  max_age: 30
  compress: true
EOF
chmod 600 "$CONFIG_FILE"
info "Config: $CONFIG_FILE (chmod 600)"

fi  # end of SKIP_SETUP

# ─── [8/8] Install and start service ─────────────────────────────────────────

echo ""
echo "=== [8/8] Install and start service ==="
if [ "$SKIP_SETUP" = true ]; then
    # Existing config — just restart
    $BIN_DEST service stop 2>/dev/null || true
    $BIN_DEST service start
else
    # Full setup — reinstall
    $BIN_DEST service stop 2>/dev/null || true
    $BIN_DEST service install --config "$CONFIG_FILE" 2>/dev/null || true
    $BIN_DEST service start
fi
sleep 1
systemctl status "$SERVICE_NAME" --no-pager 2>/dev/null || $BIN_DEST service status

echo ""
echo "Done."
echo "  Binary:   $BIN_DEST"
echo "  Config:   $CONFIG_FILE"
echo "  Service:  $SERVICE_NAME (interval=${INTERVAL:-3600}s)"
echo "  Log:      $LOG_FILE"
echo "  Backup:   $BACKUP_DIR"
echo ""
echo "Commands:"
echo "  systemctl status $SERVICE_NAME    # Check status"
echo "  journalctl -u $SERVICE_NAME -f    # Live log"
echo "  systemctl restart $SERVICE_NAME   # Restart"
