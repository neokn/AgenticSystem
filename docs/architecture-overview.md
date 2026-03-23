# 架構總覽：三層 Agentic System

> 本文件描述 AgenticSystem 的完整架構設計，涵蓋觸發層、Root Agent、Workflow + Agent 層的運作方式。

---

## 目錄

1. [設計理念](#設計理念)
2. [三層架構](#三層架構)
3. [Agent 類型](#agent-類型)
4. [兩層 Loop 機制](#兩層-loop-機制)
5. [元件關係圖](#元件關係圖)
6. [Session State 通訊機制](#session-state-通訊機制)
7. [Transfer 與 Escalate](#transfer-與-escalate)
8. [目錄結構對照](#目錄結構對照)

---

## 設計理念

本系統基於 Google ADK (Agent Development Kit) v1.0.0 建構，採用以下核心原則：

1. **宣告式設定**：Agent tree 結構以 YAML 設定檔描述，不寫死在 Go 程式碼中
2. **純函數式 Agent**：每個 agent 像純函數 — 讀 state、做事、寫 state，透過 session state 溝通
3. **通用 Workflow 骨架**：Workflow pattern 不綁特定 domain，換 prompt 和 tool 就變成不同用途
4. **Explicit Architecture**：遵循 Hexagonal / Ports & Adapters 架構，依賴方向永遠向內

---

## 三層架構

```
                        ┌─────────────────────────────────────────┐
                        │            觸發層 (Trigger)               │
                        │                                         │
                        │  Telegram Bot  │  CLI  │  Web  │  Cron  │
                        └────────┬────────┬───────┬───────┬───────┘
                                 │        │       │       │
                                 ▼        ▼       ▼       ▼
                        ┌─────────────────────────────────────────┐
                        │         Root Agent (LlmAgent)            │
                        │                                         │
                        │  - 接收所有請求                           │
                        │  - 意圖判斷                              │
                        │  - 簡單請求：直接處理                     │
                        │  - 複雜請求：transfer 給 workflow         │
                        │                                         │
                        └──┬──────────┬──────────┬────────────────┘
                           │          │          │
                  transfer │ transfer │ transfer │
                           ▼          ▼          ▼
              ┌────────────┐ ┌────────────┐ ┌────────────────────┐
              │Plan-Execute │ │  Iterative │ │  Plan-Iterate      │
              │  -Report    │ │ Refinement │ │    -Report         │
              │(Sequential) │ │  (Loop)    │ │(Sequential+Loop)   │
              ├─────────────┤ ├────────────┤ ├────────────────────┤
              │  Planner    │ │┌──────────┐│ │  Planner           │
              │  Executor   │ ││ Worker   ││ │  LoopAgent         │
              │  Reporter   │ ││ Evaluator││ │   ├─ Worker        │
              └─────────────┘ │└──────────┘│ │   └─ Evaluator     │
                              └────────────┘ │  Reporter          │
                                             └────────────────────┘
```

### 第一層：觸發層 (Trigger Layer)

觸發層是系統的外部介面，負責將不同來源的輸入轉譯為 ADK Runner 的呼叫。

| 觸發來源 | 實作位置 | 說明 |
|---------|---------|------|
| Telegram Bot | `cmd/telegram/` | Long polling，將使用者訊息轉為 agent 呼叫 |
| CLI | `cmd/agent/` | 互動式終端，stdin/stdout 驅動 |
| Web UI | `cmd/web/` | HTTP server，未來的 web 介面 |
| 排程器 | (規劃中) | 週期性喚醒 Root，用於長期任務的 heartbeat |

所有觸發來源都呼叫同一個 `application.New()` 組裝核心，只是 I/O 層不同。

### 第二層：Root Agent

Root Agent 是系統的唯一入口，具備以下職責：

1. **意圖判斷**：分析使用者請求的複雜度和類型
2. **路由決策**：決定自己處理還是轉交給 workflow
3. **直接處理**：簡單問答、閒聊、單步查詢自己回答
4. **結果彙整**：當 workflow 執行完畢 (escalate)，讀取 state 整理回覆

路由規則：
- 簡單請求 --> Root 直接處理
- 需要「規劃 + 執行 + 報告」 --> `plan_execute_report`
- 需要「迭代改進」 --> `iterative_refinement`
- 需要「規劃 + 迭代執行 + 報告」 --> `plan_iterate_report`

### 第三層：Workflow + Agent 層

Workflow 層由 ADK 的 workflow agent types 組合而成：

- **SequentialAgent**：固定步驟的多階段任務
- **LoopAgent**：反覆逼近目標的迭代任務
- **ParallelAgent**：併行多分支後彙整
- **LlmAgent**：末端具體執行者，掛載 tools 和 MCP toolsets

每個 workflow 內的 LlmAgent 透過 `output_key` 將輸出寫入 session state，
下一個 agent 透過 prompt 中的 `{key?}` 佔位符讀取，形成資料流。

---

## Agent 類型

| 類型 | ADK 對應 | 用途 | SubAgents |
|------|---------|------|-----------|
| `llm` | `llmagent.New()` | 具備 LLM 推理能力的 agent，可掛載 tools | 作為 transfer 目標 |
| `sequential` | `sequentialagent.New()` | 依序執行所有 sub-agents | 按列表順序執行 |
| `loop` | `loopagent.New()` | 重複執行 sub-agents 直到 escalate 或達到上限 | 每輪按順序執行 |
| `parallel` | `parallelagent.New()` | 併行執行所有 sub-agents | 同時執行 |

---

## 兩層 Loop 機制

### Task-level Agentic Loop（ADK LlmAgent 內建）

每次任務內：
```
read state -> think -> call tools/sub-agents -> update state -> 判斷完成
```
生命週期跟著任務走，達成條件或安全上限就結束。

### System-level Heartbeat（排程器週期性喚醒 Root）

每次喚醒開一輪新任務，跑完就結束，等下次觸發。
保證長期使命必達，不依賴永駐 process。

---

## 元件關係圖

```
                    ┌──────────────────────────────────────┐
                    │              cmd/ 觸發層               │
                    │  telegram/  │  agent/  │  web/        │
                    └──────┬──────┴────┬─────┴──────┬──────┘
                           │           │            │
                           ▼           ▼            ▼
                    ┌──────────────────────────────────────┐
                    │     internal/core/application/        │
                    │                                      │
                    │  wire.go (組裝入口)                    │
                    │    │                                  │
                    │    ├─ agenttree.yaml ──┐              │
                    │    │   (宣告式設定)     │              │
                    │    │                   ▼              │
                    │    │  agenttree/builder.go            │
                    │    │   (遞迴建構 agent tree)           │
                    │    │                                  │
                    │    ├─ ADK Runner                      │
                    │    ├─ Session Service                 │
                    │    └─ Plugins (memory, debug)         │
                    └──────────────┬───────────────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              │                    │                    │
              ▼                    ▼                    ▼
    ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
    │  core/domain │    │  core/port   │    │  infra/      │
    │              │    │              │    │              │
    │ AgentTree    │    │ AgentLoader  │    │ agentdef/    │
    │   Config     │◄───│ ToolProvider │◄───│ agenttree/   │
    │ StateKeys    │    │              │    │ mcpconfig/   │
    │ Validation   │    │              │    │ shell/       │
    └──────────────┘    └──────────────┘    │ memory/      │
                                            └──────────────┘
```

### 資料流

```
使用者訊息
    │
    ▼
cmd/telegram (Driving Adapter)
    │
    ▼
application.New() → 載入 agenttree.yaml
    │
    ▼
agenttree.Build() → 遞迴建構 agent tree
    │
    ├─ agentdef.Load() → 載入每個 agent 的 prompt
    ├─ gemini.NewModel() → 建立 LLM model
    ├─ mcptoolset.New() → 建立 MCP toolsets
    └─ 組合 Sequential / Loop / Parallel / LLM agents
    │
    ▼
runner.Run(userMessage)
    │
    ▼
Root Agent 意圖判斷
    │
    ├─ 簡單 → Root 直接回覆
    └─ 複雜 → transfer 給 workflow
              │
              ▼
         Workflow 執行 (Sequential / Loop)
              │
              ├─ 每個 LlmAgent 寫 state (output_key)
              └─ 最後一個 agent escalate
              │
              ▼
         Root 讀取 state → 整理回覆
              │
              ▼
         cmd/telegram → 發送到 Telegram
```

---

## Session State 通訊機制

Agent 之間透過 session state 溝通。每個 LlmAgent 可以設定 `output_key`，
其文字輸出會自動存入該 key。下一個 agent 透過 prompt 中的 `{key?}` 讀取。

### 標準 State Keys

| Key | 寫入者 | 讀取者 | 用途 |
|-----|--------|--------|------|
| `user_intent` | Root Agent | Planner | 解析後的使用者意圖 |
| `plan` | Planner | Executor, Worker | 結構化執行計畫 |
| `artifacts` | Executor | Reporter | 執行步驟的產出 |
| `draft` | Worker | Evaluator | 迭代中的工作草稿 |
| `evaluation` | Evaluator | Worker | 評估回饋 |
| `summary` | Reporter | Root Agent | 最終摘要 |

### State 流動方向

**Plan-Execute-Report:**
```
user_intent → plan → artifacts → summary
```

**Iterative Refinement:**
```
draft → evaluation → (不滿意) → draft → evaluation → ... → (滿意, escalate)
```

**Nested (Plan-Iterate-Report):**
```
user_intent → plan → [draft → evaluation → ...]* → summary
```

---

## Transfer 與 Escalate

### Transfer（向下轉交）

Root Agent 透過 ADK 的 agent transfer 機制將控制權交給 sub-agent。
ADK 自動設定 parent-child 關係，LLM 根據 sub-agent 的 `description` 決定轉交目標。

```yaml
# agenttree.yaml 中的 description 是 LLM 做路由決策的依據
sub_agents:
  - name: plan_execute_report
    description: "Handles complex tasks that need planning, execution, and summarization."
```

### Escalate（向上回傳）

LoopAgent 中的 sub-agent 設定 `event.Actions.Escalate = true` 即可結束迴圈。
在 prompt 中指示 Evaluator agent 在品質達標時 escalate。

SequentialAgent 最後一個 agent 執行完畢後，控制權自動回到 parent。
如果需要提前結束，agent 可以透過 escalate 中斷。

---

## 目錄結構對照

```
AgenticSystem/
├── agenttree.yaml                          # 宣告式 agent tree 設定
├── agents/                                 # Agent 定義 (data, not code)
│   ├── root/agent.prompt                   #   Root Agent prompt
│   ├── planner/agent.prompt                #   Planner prompt
│   ├── executor/agent.prompt               #   Executor prompt
│   ├── reporter/agent.prompt               #   Reporter prompt
│   ├── worker/agent.prompt                 #   Worker prompt
│   ├── evaluator/agent.prompt              #   Evaluator prompt
│   └── demo_agent/                         #   Legacy demo agent
│       ├── agent.prompt
│       └── mcp.json
├── internal/
│   ├── core/
│   │   ├── domain/
│   │   │   ├── model.go                    #   原有 domain types
│   │   │   └── agenttree.go                #   Agent tree config types + validation
│   │   ├── port/
│   │   │   ├── agent.go                    #   AgentLoader port
│   │   │   └── tool.go                     #   ToolProvider port
│   │   └── application/
│   │       ├── wire.go                     #   組裝入口（支援雙模式）
│   │       └── agenttree/
│   │           └── builder.go              #   遞迴建構 agent tree
│   └── infra/
│       └── config/
│           ├── agentdef/                   #   dotprompt loader
│           ├── agenttree/                  #   YAML config loader
│           └── mcpconfig/                  #   MCP config loader
└── docs/
    ├── architecture-overview.md            #   本文件
    ├── agent-tree-config-guide.md          #   設定檔指南
    ├── workflow-patterns-guide.md          #   Workflow 設計指南
    └── end-to-end-flow.md                  #   端到端流程範例
```
