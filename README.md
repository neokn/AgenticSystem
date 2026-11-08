# Goost — ADK Go Telegram Bot

使用 [ADK Go](https://github.com/google/adk-go) 建構的 Telegram Bot，展示 Agent 開發的核心技術。

## 技術棧

### 1. ADK Go (Agent Development Kit)

使用 Google 的 ADK Go v1.1.0 建構 agent，核心元件：

- **`llmagent`** — 封裝 LLM agent 的 instruction、tools、callbacks
- **`runner.Runner`** — 管理 agent 執行迴圈，自動處理 multi-turn conversation
- **`session.InMemoryService`** — 記憶體內 session 管理，自動維護每位使用者的對話歷史

### 2. Dotprompt

使用 [google/dotprompt](https://github.com/google/dotprompt) 管理 system prompt：

- `.prompt` 檔案以 YAML frontmatter 定義 model 名稱等 metadata
- Handlebars 模板語法支援 `{{role "system"}}` 等 role markers
- Prompt 與程式碼分離，方便迭代調整

```
agents/goost/agent.prompt
```

### 3. ADK Go Dev UI

ADK Go 內建開發者 UI，可用於本地測試與除錯 agent 行為：

```bash
go run ./cmd/telegram/ web
```

### 4. Custom Tool — `get_current_time`

使用 `functiontool.New` 建立自訂 tool，讓 LLM 取得正確的當前時間，避免幻覺：

- 支援 IANA timezone 參數（預設 `Asia/Taipei`）
- 回傳 RFC3339 格式的時間字串

### 5. MCP (Model Context Protocol)

透過 ADK Go 的 `mcptoolset` 整合外部 MCP server：

- 使用 `mcp.CommandTransport` 透過 stdio 啟動子行程
- 目前掛載 **chrome-devtools-mcp**，提供瀏覽器操作能力
- Lazy connection — 只在 LLM 需要時才啟動 MCP server

## 快速開始

```bash
# 1. 複製環境變數
cp template.env .env

# 2. 填入 API keys
#    GOOGLE_GENAI_API_KEY=<your-key>
#    GOOGLE_GENAI_USE_VERTEXAI=TRUE
#    TELEGRAM_BOT_TOKEN=<your-bot-token>
```

## 啟動方式

程式內建兩種模式，共用同一個 agent：

```bash
# Telegram Bot（預設，不帶參數）
go run ./cmd/telegram/

# ADK Dev UI（本地測試與除錯）
go run ./cmd/telegram/ web --port 9090 webui -api_server_address http://localhost:9090/api api

# ADK Console（CLI 互動模式）
go run ./cmd/telegram/ console
```

> Telegram 模式需要設定 `TELEGRAM_BOT_TOKEN`；Dev UI / Console 模式只需要 `GOOGLE_GENAI_API_KEY`。

## 專案結構

```
.
├── agents/goost/agent.prompt   # Dotprompt system prompt
├── cmd/telegram/main.go        # 主程式入口
├── template.env                # 環境變數範本
└── go.mod
```
