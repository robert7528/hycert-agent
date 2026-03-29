# HyCert Agent — Linux 安裝指南

## 系統需求

- Linux（amd64 或 arm64）
- 可連線到 hycert-api server
- root 權限
- jq、curl 已安裝

## 快速安裝（3 步）

### 1. 取得安裝檔

**方式 A：從 GitHub Release 下載（推薦）**

```bash
# amd64（一般主機）
curl -L -o hycert-agent.tar.gz \
  https://github.com/robert7528/hycert-agent/releases/latest/download/hycert-agent-v0.2.0-linux-amd64.tar.gz

# arm64（AWS ARM 等）
curl -L -o hycert-agent.tar.gz \
  https://github.com/robert7528/hycert-agent/releases/latest/download/hycert-agent-v0.2.0-linux-arm64.tar.gz
```

> 版本號請替換為最新版，查看：https://github.com/robert7528/hycert-agent/releases

**方式 B：從已 build 的主機打包**

```bash
tar -czvf /tmp/hycert-agent.tgz \
  /hysp/hycert-agent/bin/hycert-agent-linux-amd64 \
  /hysp/hycert-agent/deployment/deploy.sh
```

### 2. 解壓

```bash
sudo mkdir -p /hysp/hycert-agent/bin /hysp/hycert-agent/deployment
cd /hysp/hycert-agent

# GitHub Release 下載的：
sudo tar xzf /tmp/hycert-agent.tar.gz
# 產生：hycert-agent-linux-amd64（或 arm64）、deploy.sh、README.md

# 搬到正確位置
ARCH=$(uname -m)
case "$ARCH" in aarch64|arm64) BIN=hycert-agent-linux-arm64 ;; *) BIN=hycert-agent-linux-amd64 ;; esac
sudo mv $BIN bin/ 2>/dev/null || true
sudo mv deploy.sh deployment/ 2>/dev/null || true
```

### 3. 執行安裝

```bash
sudo bash /hysp/hycert-agent/deployment/deploy.sh
```

安裝腳本會引導你輸入：

| 步驟 | 輸入項目 | 預設值 |
|------|----------|--------|
| 1 | Server URL | （必填，例如 `https://jumper.k00.com.tw/hycert-api`） |
| 2 | HTTP Proxy | 空（無代理） |
| 3 | Skip SSL verify | N |
| 4 | 租戶代碼 | system |
| 5 | 管理者帳號 | admin |
| 6 | 管理者密碼 | （必填） |
| 7 | 標籤 label | 建議填入客戶代碼 |
| 8 | Agent 顯示名稱 | hostname |
| 9 | 輪詢間隔（秒） | 3600 |

完成後服務自動啟動。

## 安裝後驗證

### 確認服務狀態

```bash
sudo systemctl status hycert-agent
```

### 手動執行一次（測試連線 + 部署）

```bash
sudo /usr/local/bin/hycert-agent run --config /etc/hycert/agent.yaml
```

### 查看 agent-id

```bash
cat /etc/hycert/agent-id
```

## 檔案位置

| 檔案 | 路徑 | 說明 |
|------|------|------|
| Binary | `/usr/local/bin/hycert-agent` | 主程式 |
| Config | `/etc/hycert/agent.yaml` | 設定檔（chmod 600） |
| Agent ID | `/etc/hycert/agent-id` | UUID + machine-id（自動產生，勿刪除） |
| Log | `/var/log/hycert-agent/agent.log` | 日誌（自動 rotation） |
| Backup | `/var/lib/hycert-agent/backups/` | 憑證備份 |

## 常用指令

```bash
# 服務管理
sudo systemctl status hycert-agent     # 查看狀態
sudo systemctl restart hycert-agent    # 重啟
sudo systemctl stop hycert-agent       # 停止

# 手動操作
sudo /usr/local/bin/hycert-agent run --config /etc/hycert/agent.yaml   # 單次執行
sudo /usr/local/bin/hycert-agent version                                # 查看版本

# 查看 Log
sudo tail -f /var/log/hycert-agent/agent.log     # 即時 log
sudo journalctl -u hycert-agent -f                # systemd journal
```

## 重裝 / 更新

直接再跑一次安裝腳本：

```bash
sudo bash /hysp/hycert-agent/deployment/deploy.sh
```

- 偵測到現有設定 → 選 1 繼續使用（只重啟服務）
- 偵測到現有設定 → 選 2 重新設定（重新輸入所有參數）
- 如需更新 binary，從 GitHub Release 下載最新版替換 `/hysp/hycert-agent/bin/` 下的 binary 再跑腳本

## 移除

```bash
sudo /usr/local/bin/hycert-agent service stop
sudo /usr/local/bin/hycert-agent service uninstall
sudo rm /usr/local/bin/hycert-agent
sudo rm -rf /etc/hycert /var/log/hycert-agent /var/lib/hycert-agent
```

## 疑難排解

| 問題 | 排查 |
|------|------|
| 註冊失敗 | 檢查 server URL、token、網路連線（proxy） |
| 部署失敗 | 檢查 cert_path/key_path 路徑和權限、reload_cmd 是否正確 |
| 服務無法啟動 | `journalctl -u hycert-agent -n 50` 查看錯誤 |
| no deployments found | 到 UI 建立部署目標並確認 label 匹配 |
