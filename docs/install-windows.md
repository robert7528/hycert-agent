# HyCert Agent — Windows 安裝指南

## 系統需求

- Windows Server 2012+ 或 Windows 10+（amd64）
- 可連線到 hycert-api server
- 系統管理員權限

## 快速安裝

### 方式一：使用 deploy-windows.ps1（推薦）

1. 建立目錄 `D:\hycert-agent`
2. 將以下檔案放入該目錄：
   - `hycert-agent-windows-amd64.exe`
   - `deploy-windows.ps1`
3. 以**系統管理員**開啟 PowerShell：

```powershell
cd D:\hycert-agent
.\deploy-windows.ps1
```

腳本會互動式引導：
1. 停止現有服務（如果有）
2. 建立目錄（logs、backups）
3. 詢問設定（名稱、URL、proxy、token）
4. 寫入 config
5. 安裝 Windows 服務
6. 執行測試 + 啟動服務

### 方式二：手動安裝

```powershell
# 1. 建立目錄
New-Item -ItemType Directory -Path D:\hycert-agent\logs -Force
New-Item -ItemType Directory -Path D:\hycert-agent\backups -Force

# 2. 複製 binary 到 D:\hycert-agent\hycert-agent-windows-amd64.exe

# 3. 建立 config（D:\hycert-agent\agent.yaml）
# 參考下方 Config 說明

# 4. 測試單次執行
.\hycert-agent-windows-amd64.exe run --config D:\hycert-agent\agent.yaml

# 5. 安裝並啟動服務（需要管理員權限）
.\hycert-agent-windows-amd64.exe service install --config D:\hycert-agent\agent.yaml
.\hycert-agent-windows-amd64.exe service start
```

## 檔案位置

| 檔案 | 路徑 | 說明 |
|------|------|------|
| Binary | `D:\hycert-agent\hycert-agent-windows-amd64.exe` | 主程式 |
| Config | `D:\hycert-agent\agent.yaml` | 設定檔 |
| Agent ID | `D:\hycert-agent\agent-id` | UUID（自動產生，勿刪除） |
| Log | `D:\hycert-agent\logs\agent.log` | 日誌（lumberjack 自動 rotation） |
| Backup | `D:\hycert-agent\backups\` | 憑證備份（按 deployment 分子目錄） |

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
  backup_dir: "D:/hycert-agent/backups"

log:
  level: "info"                          # debug / info / warn / error
  file: "D:/hycert-agent/logs/agent.log"
  max_size: 10                           # MB，超過自動 rotation
  max_backups: 3                         # 保留幾份舊 log
  max_age: 30                            # 天，超過自動刪除
  compress: true                         # 壓縮舊 log
```

- `agent_id` 不需要手動設定，首次啟動自動產生並存到 `D:\hycert-agent\agent-id`
- `token` 可用環境變數 `HYCERT_AGENT_TOKEN` 覆蓋
- 路徑可用正斜線 `/` 或反斜線 `\`（YAML 裡反斜線需要用 `\\` 跳脫）

## 指令操作

> 所有服務管理指令需要以**系統管理員**執行。

### 服務管理

```powershell
cd D:\hycert-agent

# 安裝服務（自動設為開機啟動）
.\hycert-agent-windows-amd64.exe service install --config D:\hycert-agent\agent.yaml

# 啟動
.\hycert-agent-windows-amd64.exe service start

# 停止
.\hycert-agent-windows-amd64.exe service stop

# 查看狀態
.\hycert-agent-windows-amd64.exe service status

# 移除服務
.\hycert-agent-windows-amd64.exe service uninstall
```

### 手動操作

```powershell
# 單次執行（不啟動服務，適合測試）
.\hycert-agent-windows-amd64.exe run --config D:\hycert-agent\agent.yaml

# 前台 daemon 模式（持續輪詢，Ctrl+C 停止）
.\hycert-agent-windows-amd64.exe daemon --config D:\hycert-agent\agent.yaml

# 查看版本
.\hycert-agent-windows-amd64.exe version
```

### 查看 Log

```powershell
# 即時查看（類似 tail -f）
Get-Content D:\hycert-agent\logs\agent.log -Tail 20 -Wait

# 最近 20 行
Get-Content D:\hycert-agent\logs\agent.log -Tail 20

# 搜尋錯誤
Select-String -Path D:\hycert-agent\logs\agent.log -Pattern "ERROR|WARN"
```

### Windows 服務管理（也可以用）

```powershell
# PowerShell
Get-Service hycert-agent
Restart-Service hycert-agent
Stop-Service hycert-agent
Start-Service hycert-agent

# 或到 services.msc 圖形介面管理
```

## Proxy 設定

如果主機需要透過 proxy 連線到 hycert-api，在 config 中設定：

```yaml
server:
  proxy: "http://192.168.5.249:1654"
```

不需要設定系統環境變數，agent 會自動使用 config 中的 proxy。

## 憑證部署範例

### Tomcat（JKS）

在 hycert UI 建立 deployment：
- **Agent**: 選擇此主機的 agent
- **服務類型**: tomcat
- **憑證路徑**: `D:\server.jks`
- **Keystore 密碼**: 與 Tomcat server.xml 的 `keystorePass` 一致
- **Key 別名**: 與 Tomcat server.xml 的 `keyAlias` 一致
- **重載指令**: `Restart-Service Tomcat8`

### Nginx（PEM）

- **服務類型**: nginx
- **憑證路徑**: `C:\nginx\ssl\cert.pem`
- **私鑰路徑**: `C:\nginx\ssl\key.pem`
- **重載指令**: `nginx -s reload`

### IIS（PFX，Phase 2）

尚未實作，規劃中。

## 更新 Agent

```powershell
# 以管理員執行
cd D:\hycert-agent
.\hycert-agent-windows-amd64.exe service stop
# 複製新的 exe 覆蓋
.\hycert-agent-windows-amd64.exe service start
```

或重跑 `.\deploy-windows.ps1`（會自動停服務再更新）。

## 疑難排解

### Agent 註冊失敗
- 檢查 server URL 是否正確
- 檢查 proxy 設定（公司內網通常需要 proxy）
- 檢查 token 是否有效
- 嘗試 `insecure_skip_verify: true`（自簽憑證環境）

### 部署失敗 — Access is denied
- 確認以**管理員**身分執行或安裝服務
- 檢查 cert_path 的寫入權限
- 確認 keystore 檔案沒有被其他程式鎖定

### 部署失敗 — 服務重啟失敗
- 確認 reload_cmd 中的服務名稱正確
- 用 `Get-Service | Where-Object { $_.DisplayName -like "*Tomcat*" }` 確認服務名稱

### 查看詳細 log
```powershell
# 改 config 的 log level 為 debug
(Get-Content D:\hycert-agent\agent.yaml) -replace 'level: "info"', 'level: "debug"' | Set-Content D:\hycert-agent\agent.yaml
Restart-Service hycert-agent
```
