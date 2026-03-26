# HyCert Agent — Linux 安裝指南

## 系統需求

- Linux（amd64）
- 可連線到 hycert-api server
- root 權限
- jq、curl 已安裝

## 快速安裝（3 步）

### 1. 取得安裝檔

從已 build 的主機打包：

```bash
# 在 build 主機上（例如 10.30.0.70）
tar -czvf /tmp/hycert-agent.tgz \
  /hysp/hycert-agent/bin/hycert-agent-linux-amd64 \
  /hysp/hycert-agent/deployment/deploy.sh
```

將 `hycert-agent.tgz` 傳到客戶端主機（FTP、SCP 等）。

### 2. 解壓

```bash
cd /
sudo tar -xzvf /tmp/hycert-agent.tgz
```

解壓後目錄結構：
```
/hysp/hycert-agent/
├── bin/hycert-agent-linux-amd64    # 主程式
└── deployment/deploy.sh            # 安裝腳本
```

### 3. 執行安裝

```bash
sudo bash /hysp/hycert-agent/deployment/deploy.sh
```

安裝腳本會引導你輸入：

| 步驟 | 輸入項目 | 預設值 |
|------|----------|--------|
| 1 | Server URL | （必填，例如 `https://domain/hycert-api`） |
| 2 | HTTP Proxy | 空（無代理） |
| 3 | Skip SSL verify | N |
| 4 | 租戶代碼 | system |
| 5 | 管理者帳號 | admin |
| 6 | 管理者密碼 | （必填） |
| 7 | 標籤 label | 空（可留空） |
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
- 如需更新 binary，先替換 `/hysp/hycert-agent/bin/hycert-agent-linux-amd64` 再跑腳本

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
