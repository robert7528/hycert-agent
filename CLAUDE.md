# hycert-agent

HyCert 部署代理客戶端，定期從 hycert-api 檢查憑證更新並自動部署到目標主機。

## 技術棧
- Go（交叉編譯 linux/amd64 + windows/amd64）
- Cobra（CLI）、Viper（config）、slog（log）
- 零外部依賴（除 Cobra/Viper 外全用 stdlib）

## 建構
```bash
make build-all    # 同時編譯 linux + windows
make build-linux  # 只編譯 linux
make build-windows # 只編譯 windows
```

## 使用
```bash
# 單次執行（cron）
hycert-agent run --config /etc/hycert/agent.yaml

# 持續輪詢（systemd）
hycert-agent daemon --config /etc/hycert/agent.yaml
```

## 設定檔
見 `configs/agent.example.yaml`。Token 可用 `HYCERT_AGENT_TOKEN` 環境變數覆蓋。

## 對應 API
- `GET /api/v1/agent/cert/deployments?host={hostname}`
- `GET /api/v1/agent/cert/certificates/:id/download?format=pem`
- `PUT /api/v1/agent/cert/deployments/:id/status`

## Phase 1 支援的服務
| target_service | 部署方式 |
|---|---|
| nginx | cert + key 分開檔案 |
| apache | cert + key 分開檔案 |
| haproxy | cert+key 合併單檔 |
| hyproxy | cert+key 合併單檔 |
