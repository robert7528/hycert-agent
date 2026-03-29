# HyCert Agent — Windows 安裝指南

## 系統需求

- Windows Server 2012+ 或 Windows 10+（amd64）
- 可連線到 hycert-api server
- Administrator 權限

## 快速安裝（3 步）

### 1. 取得安裝檔

**方式 A：從 GitHub Release 下載（推薦）**

下載最新版：https://github.com/robert7528/hycert-agent/releases

選擇 `hycert-agent-vX.X.X-windows-amd64.zip`

**方式 B：由管理員提供 zip 檔**

### 2. 解壓

解壓到 `D:\hycert-agent\`：

```
D:\hycert-agent\
├── hycert-agent-windows-amd64.exe    # 主程式
├── deploy-windows.ps1                 # 安裝腳本
└── install.bat                        # 一鍵啟動器
```

### 3. 執行安裝

雙擊 `install.bat` → 點 UAC「是」→ 依提示輸入設定。

| 步驟 | 輸入項目 | 預設值 |
|------|----------|--------|
| 1 | Server URL | （必填，例如 `https://jumper.k00.com.tw/hycert-api`） |
| 2 | HTTP Proxy | 空（無代理） |
| 3 | Skip SSL verify | N |
| 4 | 租戶代碼 | system |
| 5 | 管理者帳號 | admin |
| 6 | 管理者密碼 | （必填） |
| 7 | 標籤 label | 建議填入客戶代碼 |
| 8 | Agent 顯示名稱 | 電腦名稱 |
| 9 | 輪詢間隔（秒） | 3600 |

完成後服務自動啟動。

## 安裝後驗證

以 Administrator 開啟 PowerShell：

```powershell
cd D:\hycert-agent

# 確認服務狀態
.\hycert-agent-windows-amd64.exe service status

# 手動執行一次（測試連線 + 部署）
.\hycert-agent-windows-amd64.exe run --config D:\hycert-agent\agent.yaml

# 查看 agent-id
Get-Content D:\hycert-agent\agent-id
```

## 檔案位置

| 檔案 | 路徑 | 說明 |
|------|------|------|
| Binary | `D:\hycert-agent\hycert-agent-windows-amd64.exe` | 主程式 |
| Config | `D:\hycert-agent\agent.yaml` | 設定檔 |
| Agent ID | `D:\hycert-agent\agent-id` | UUID + machine-id（自動產生，勿刪除） |
| Log | `D:\hycert-agent\logs\agent.log` | 日誌（自動 rotation） |
| Backup | `D:\hycert-agent\backups\` | 憑證備份 |

## 常用指令

以 Administrator 開啟 PowerShell，切換到 `D:\hycert-agent`：

```powershell
# 服務管理
.\hycert-agent-windows-amd64.exe service status    # 查看狀態
.\hycert-agent-windows-amd64.exe service stop      # 停止
.\hycert-agent-windows-amd64.exe service start     # 啟動

# 手動操作
.\hycert-agent-windows-amd64.exe run --config D:\hycert-agent\agent.yaml   # 單次執行
.\hycert-agent-windows-amd64.exe version                                    # 查看版本

# 查看 Log
Get-Content D:\hycert-agent\logs\agent.log -Tail 20    # 最近 20 行
Get-Content D:\hycert-agent\logs\agent.log -Wait        # 即時 log
```

## 重裝 / 更新

雙擊 `install.bat` 再跑一次：

- 偵測到現有設定 → 選 1 繼續使用（只重啟服務）
- 偵測到現有設定 → 選 2 重新設定（重新輸入所有參數）
- 如需更新 binary，從 GitHub Release 下載最新版替換 `hycert-agent-windows-amd64.exe` 再跑 `install.bat`

## 移除

以 Administrator 開啟 PowerShell：

```powershell
cd D:\hycert-agent
.\hycert-agent-windows-amd64.exe service stop
.\hycert-agent-windows-amd64.exe service uninstall
Remove-Item D:\hycert-agent -Recurse -Force
```

## 疑難排解

| 問題 | 排查 |
|------|------|
| install.bat 沒反應 | 確認以 Administrator 執行（右鍵 → 以系統管理員身分執行） |
| 登入失敗 | 檢查 Server URL、帳號密碼、網路連線（proxy） |
| 服務無法啟動 | `.\hycert-agent-windows-amd64.exe run` 看錯誤訊息 |
| no deployments found | 到 UI 建立部署目標並確認 label 匹配 |
| 密碼無法貼上 | 直接手動輸入密碼 |
