# HyCert Agent — Linux 安裝指南

## 系統需求

- Linux（amd64）
- 可連線到 hycert-api server
- root 權限

## 快速安裝

### 方式一：使用 deploy.sh（推薦）

```bash
# 1. 下載或 clone repo
cd /hysp
git clone https://github.com/robert7528/hycert-agent.git
cd hycert-agent

# 2. 編譯
make build-linux

# 3. 一鍵安裝（互動式，會自動建立 config、安裝服務、啟動）
sudo bash deployment/deploy.sh
```

deploy.sh 會依序執行：
1. 安裝 binary 到 `/usr/local/bin/hycert-agent`
2. 建立目錄（config、backup、log）
3. 登入 hyadmin-api 取得 JWT
4. 建立 Agent Token（或沿用現有 token）
5. 寫入 config（`/etc/hycert/agent.yaml`）
6. 安裝 systemd 服務
7. 檢查 deployments 並執行測試
8. 啟動服務

### 方式二：手動安裝

```bash
# 1. 複製 binary
cp bin/hycert-agent-linux-amd64 /usr/local/bin/hycert-agent
chmod +x /usr/local/bin/hycert-agent

# 2. 建立目錄
mkdir -p /etc/hycert /var/log/hycert-agent /var/lib/hycert-agent/backups

# 3. 建立 config
cat > /etc/hycert/agent.yaml << 'EOF'
server:
  url: "https://your-server/hycert-api"
  token: "hycert_agt_xxxxx..."
  proxy: ""
  insecure_skip_verify: false

agent:
  name: "my-server"
  interval: 3600
  backup: true
  backup_dir: "/var/lib/hycert-agent/backups"

log:
  level: "info"
  file: "/var/log/hycert-agent/agent.log"
  max_size: 10
  max_backups: 3
  max_age: 30
  compress: true
EOF
chmod 600 /etc/hycert/agent.yaml

# 4. 測試單次執行
hycert-agent run --config /etc/hycert/agent.yaml

# 5. 安裝並啟動服務
hycert-agent service install --config /etc/hycert/agent.yaml
hycert-agent service start
```

## 檔案位置

| 檔案 | 路徑 | 說明 |
|------|------|------|
| Binary | `/usr/local/bin/hycert-agent` | 主程式 |
| Config | `/etc/hycert/agent.yaml` | 設定檔 |
| Agent ID | `/etc/hycert/agent-id` | UUID（自動產生，勿刪除） |
| Log | `/var/log/hycert-agent/agent.log` | 日誌（lumberjack 自動 rotation） |
| Backup | `/var/lib/hycert-agent/backups/` | 憑證備份（按 deployment 分子目錄） |

## Config 說明

```yaml
server:
  url: "https://your-server/hycert-api"  # hycert-api 的 URL
  token: "hycert_agt_xxxxx..."           # Agent Token（從 UI 或 API 取得）
  proxy: "http://proxy:port"             # HTTP proxy（選填）
  insecure_skip_verify: false            # 跳過 SSL 驗證（自簽憑證用）

agent:
  name: "my-server"                      # 顯示名稱（UI 上看到的）
  interval: 3600                         # 輪詢間隔（秒）
  backup: true                           # 啟用備份
  backup_dir: "/var/lib/hycert-agent/backups"

log:
  level: "info"                          # debug / info / warn / error
  file: "/var/log/hycert-agent/agent.log"
  max_size: 10                           # MB，超過自動 rotation
  max_backups: 3                         # 保留幾份舊 log
  max_age: 30                            # 天，超過自動刪除
  compress: true                         # 壓縮舊 log
```

- `agent_id` 不需要手動設定，首次啟動自動產生並存到 `/etc/hycert/agent-id`
- `token` 可用環境變數 `HYCERT_AGENT_TOKEN` 覆蓋

## 指令操作

### 服務管理

```bash
# 安裝服務（自動產生 systemd unit + 設為開機啟動）
hycert-agent service install --config /etc/hycert/agent.yaml

# 啟動
hycert-agent service start

# 停止
hycert-agent service stop

# 查看狀態
hycert-agent service status

# 移除服務
hycert-agent service uninstall
```

### 手動操作

```bash
# 單次執行（不啟動服務，適合 cron 或測試）
hycert-agent run --config /etc/hycert/agent.yaml

# 前台 daemon 模式（持續輪詢，Ctrl+C 停止）
hycert-agent daemon --config /etc/hycert/agent.yaml

# 查看版本
hycert-agent version
```

### 查看 Log

```bash
# 檔案 log（lumberjack rotation）
tail -f /var/log/hycert-agent/agent.log

# systemd journal
journalctl -u hycert-agent -f

# 最近 20 行
tail -20 /var/log/hycert-agent/agent.log
```

### systemd 指令（也可以用）

```bash
systemctl status hycert-agent
systemctl restart hycert-agent
systemctl stop hycert-agent
systemctl enable hycert-agent    # 開機自動啟動（install 時已設定）
systemctl disable hycert-agent   # 取消開機啟動
```

## 更新 Agent

```bash
cd /hysp/hycert-agent
git pull
make build-linux
hycert-agent service stop
cp bin/hycert-agent-linux-amd64 /usr/local/bin/hycert-agent
hycert-agent service start
```

或重跑 `sudo bash deployment/deploy.sh`（會自動停服務再更新）。

## 疑難排解

### Agent 註冊失敗
- 檢查 server URL 是否正確
- 檢查 token 是否有效
- 檢查網路連線（proxy 設定）

### 部署失敗
- 檢查 cert_path / key_path 路徑是否存在
- 檢查檔案權限（agent 需要寫入權限）
- 檢查 reload_cmd 是否正確（需要 root 權限）

### 查看詳細 log
```bash
# 改 config 的 log level 為 debug
sed -i 's/level: "info"/level: "debug"/' /etc/hycert/agent.yaml
hycert-agent service stop
hycert-agent service start
```
