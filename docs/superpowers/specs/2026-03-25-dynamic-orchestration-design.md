# Dynamic Orchestration：Root LLM 動態編排 Agent Tree

> 設計文件 — 2026-03-25

---

## 概述

將 agentic system 的編排方式從靜態 YAML config 轉為 **Root LLM 動態生成**。Root 接收使用者 prompt 後，輸出結構化 Plan JSON（包含意圖和 workflow 結構），系統解析 JSON 動態建構 ADK agent tree 並執行。Root 同時負責監督執行結果，不滿意可重新規劃（外層 loop），滿意後整理最終回覆。

---

## 設計決策摘要

| 決策 | 選擇 | 理由 |
|------|------|------|
| Root output 形式 | Structured JSON（Gemini `response_schema`） | 保證 output 為合法 JSON，可被系統直接解析 |
| 執行方式 | 系統解析 Plan JSON → 動態建構 ADK agent tree → runner 執行 | 每個 step 是真正的 LlmAgent，有獨立 context |
| Root 角色 | 規劃 + 監督 + 最終回覆（外層 loop） | 完整的端到端品質把關 |
| Step instruction 來源 | 混合：已知 role 載入 prompt template，未知 role 由 Root 即時生成 | 兼顧 reusable template 和動態彈性 |
| Workflow 結構 | 完整 sequential + loop + parallel | 覆蓋所有常見編排需求 |
| 外層 loop 結束條件 | Root 宣告 `max_retries` + 系統硬上限兜底 | Root 自主 + 安全保障 |
| 實作模式 | Two-Phase Agent（Orchestrator 驅動 plan/execute/evaluate/respond） | Plan/execute 分離乾淨，外層 loop 是確定性 Go code |
| 向後相容 | 不保留，移除 legacy 和 YAML 模式 | 乾淨切換 |

---

## 架構

### 整體流程

```
User prompt
    │
    ▼
┌─────────────────────────────────────────────────────┐
│                  Orchestrator (Go)                    │
│                                                       │
│  ┌──────────┐     ┌──────────┐     ┌──────────────┐ │
│  │ Phase 1   │     │ Phase 2   │     │ Phase 3       │ │
│  │ Plan      │────▶│ Execute   │────▶│ Evaluate      │ │
│  │           │     │           │     │               │ │
│  │ Root LLM  │     │ Dynamic   │     │ Root LLM      │ │
│  │ + schema  │     │ Agent Tree│     │ + 結果 state   │ │
│  └──────────┘     └──────────┘     └───────┬───────┘ │
│       ▲                                      │        │
│       │              不滿意                   │        │
│       └──────────────────────────────────────┘        │
│                        │ 滿意                          │
│                        ▼                               │
│                 Phase 4: Respond                       │
│                 Root LLM → 自然語言回覆                 │
└─────────────────────────────────────────────────────────┘
    │
    ▼
User 收到回覆
```

### 四個 Phase

| Phase | 執行者 | Input | Output |
|-------|--------|-------|--------|
| 1. Plan | Root LLM（structured output） | user prompt + (上一輪 feedback) + 可用 tools/roles 清單 | Plan JSON |
| 2. Execute | 系統動態建構的 ADK agent tree | Plan JSON → agents | session state（各 step output_key） |
| 3. Evaluate | Root LLM（structured output） | user prompt + 執行結果 state | `{satisfied: bool, feedback: string}` |
| 4. Respond | Root LLM（free-form） | user prompt + 最終結果 state | 自然語言回覆 |

### 快速路徑

Phase 1 回傳 `{ "type": "direct", "response": "..." }` 時，跳過 Phase 2/3/4，直接回傳 response。

### 外層 Loop 控制

- `satisfied: false` → 帶 `feedback` 回 Phase 1 重新規劃
- `satisfied: true` → 進入 Phase 4
- 安全上限：`min(plan.max_retries, system_hard_limit)`，超過直接進 Phase 4

---

## Plan JSON Schema

### 頂層結構

```json
{
  "intent": "string — 一句話描述使用者意圖",
  "max_retries": 2,
  "plan": { /* PlanNode — 遞迴結構 */ }
}
```

### PlanNode（遞迴）

五種 type。其中 `direct` **只能出現在頂層 `plan` 欄位**，不可作為 `sequential`/`loop`/`parallel` 的子節點。`PlanConverter` 遇到非頂層的 `direct` 必須回傳 validation error。

Gemini `response_schema` 用 union type 反映此限制：頂層 `plan` 接受 `direct | sequential | loop | parallel`，而 `steps[]` 只接受 `step | sequential | loop | parallel`。

#### `direct` — 不需要 workflow（僅限頂層）

```json
{
  "type": "direct",
  "response": "直接回覆的內容"
}
```

#### `sequential` — 依序執行

```json
{
  "type": "sequential",
  "steps": [ /* PlanNode[] */ ]
}
```

#### `loop` — 迭代直到滿足條件

```json
{
  "type": "loop",
  "max_iterations": 3,
  "exit_condition": {
    "output_key": "evaluation",
    "pattern": "APPROVED"
  },
  "steps": [ /* PlanNode[] */ ]
}
```

`exit_condition`（可選）定義提前退出條件：每次迭代結束後，系統檢查 `session_state[output_key]` 是否包含 `pattern` 字串。匹配則提前結束 loop，不匹配則繼續迭代直到 `max_iterations`。

ADK `LoopAgent` 透過 sub-agent escalation 實現提前退出。`PlanConverter` 將 `exit_condition` 轉換為 `BeforeAgentCallback`：每輪迭代開始前檢查上一輪的 state，若滿足條件則觸發 escalation 終止 loop。若未設定 `exit_condition`，loop 固定跑滿 `max_iterations`。

#### `parallel` — 併行執行

```json
{
  "type": "parallel",
  "steps": [ /* PlanNode[] */ ]
}
```

#### `step` — 末端執行節點（LlmAgent）

```json
{
  "type": "step",
  "role": "string — 角色名",
  "instruction": "string — Root 即時生成的 instruction（可選）",
  "tools": ["string — tool 名稱"],
  "output_key": "string — 結果寫入 session state 的 key"
}
```

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `type` | `"step"` | 是 | 固定值 |
| `role` | string | 是 | 有對應 `agents/<role>/agent.prompt` 就載入 template，否則用 `instruction` |
| `instruction` | string | 否 | Root 即時生成；template 存在時作為補充 context append |
| `tools` | string[] | 否 | 從系統 tool registry 查找 |
| `output_key` | string | 是 | session state key |

注意：PlanNode **沒有 `name` 欄位**。ADK 要求 agent name 唯一且非空，由 `PlanConverter` 自動生成：
- `step` → `"<role>_<index>"`（例如 `coder_0`、`coder_1`）
- `sequential` → `"seq_<index>"`
- `loop` → `"loop_<index>"`
- `parallel` → `"par_<index>"`

Index 是該節點在整棵樹中的 depth-first 序號，保證全域唯一。Root LLM 不需要關心命名。

### `{placeholder}` 語法

Step 的 `instruction` 中可用 `{output_key}` 引用其他 step 的結果。

**替換時機：Agent 執行時（非 build time）。** Instruction 在 `AgentNodeConfig` 中保持為帶 placeholder 的 template 字串。`PlanConverter` 為每個動態建構的 LlmAgent 注入一個 `BeforeAgentCallback`，在 agent 執行前從當前 session state 取值替換 `{key}` placeholder。

這對 loop 場景至關重要：第 N 次迭代的 `{evaluation}` 應取到第 N-1 次的結果，而非 build time 的空值。首次迭代若 placeholder 對應的 key 不存在，替換為空字串（agent 的 instruction 會自然處理「尚無前次 feedback」的情境）。

實作上需要擴展 Builder 的 `Deps` struct，新增一個 `BeforeAgentCallback` factory，或在 `buildLLMAgent` 中接受 callback 參數。這是 Builder **唯一需要修改的部分**。

### 巢狀組合範例

```json
{
  "type": "sequential",
  "steps": [
    { "type": "step", "role": "planner", "output_key": "plan", "instruction": "..." },
    {
      "type": "loop",
      "max_iterations": 3,
      "steps": [
        { "type": "step", "role": "coder", "tools": ["shell_exec"], "output_key": "draft", "instruction": "根據 {plan} 寫程式，參考 {evaluation} 修正" },
        { "type": "step", "role": "tester", "output_key": "evaluation", "instruction": "執行測試並評估 {draft}" }
      ]
    },
    {
      "type": "parallel",
      "steps": [
        { "type": "step", "role": "doc_writer", "output_key": "docs", "instruction": "為 {draft} 撰寫文件" },
        { "type": "step", "role": "changelog_writer", "output_key": "changelog", "instruction": "根據 {draft} 撰寫 changelog" }
      ]
    }
  ]
}
```

---

## Root LLM 的三次呼叫

Root 不是 ADK LlmAgent，而是 Orchestrator 內的三個獨立 LLM call。

### Call 1: Plan

- System prompt：`prompts/plan.prompt`
  - 任務規劃器角色
  - 注入可用 tools 清單（從 registry）
  - 注入可用 roles 清單（掃描 `agents/` 目錄）
- User content：使用者原始 prompt +（若重試）上一次 feedback
- Response schema：PlanNode JSON schema（Gemini forced structured output）

### Call 2: Evaluate

- System prompt：`prompts/evaluate.prompt`
  - 品質評估器角色
- User content：使用者原始 prompt + 各 step output（從 session state 收集）
- Response schema：`{ "satisfied": boolean, "feedback": "string" }`

### Call 3: Respond

- System prompt：`prompts/respond.prompt`
  - 回覆整理器角色
- User content：使用者原始 prompt + 最終結果 state
- Response：自然語言（free-form，無 schema）

### Prompt 管理

放在 `prompts/` 目錄（非 `agents/`），因為這三個不是 agent，是 Orchestrator 的內部 LLM call。

---

## Orchestrator 與 Builder 的關係

### Orchestrator（新元件）

驅動外層 loop 的 Go code，負責：
1. 呼叫 LLM 產生 plan
2. 將 plan 轉換為 ADK agent tree 並執行
3. 呼叫 LLM 評估結果
4. 決定重試或產出最終回覆

### PlanConverter（新元件）

將 `PlanNode` 轉換為現有的 `domain.AgentNodeConfig`，復用 Builder：

- `step` → `AgentNodeConfig{Type: "llm", ...}`
- `sequential` → `AgentNodeConfig{Type: "sequential", SubAgents: convert(steps)}`
- `loop` → `AgentNodeConfig{Type: "loop", MaxIterations: ..., SubAgents: convert(steps)}`
- `parallel` → `AgentNodeConfig{Type: "parallel", SubAgents: convert(steps)}`

Instruction 解析順序：
1. 檢查 `agents/<role>/agent.prompt` 是否存在
2. 存在 → 載入 template，`instruction` 欄位 append 為補充 context
3. 不存在 → `instruction` 欄位作為完整 system prompt
4. `{output_key}` placeholder → 從 session state 取值替換

### Builder（現有，小幅修改）

`internal/core/application/agenttree/builder.go` — 接收 `AgentNodeConfig` 遞迴建構 ADK agent tree。Input 來源從 YAML 變成 PlanConverter 的 output。

需要修改的部分：
- `Deps` struct 新增 `BeforeAgentCallback` factory，用於注入 `{placeholder}` 執行時替換邏輯
- `buildLLMAgent` 接受並掛載 callback 到 LlmAgent
- `buildLoopAgent` 接受並掛載 exit condition callback

其餘遞迴建構邏輯不變。

---

## 元件對照表

### 新增

| 元件 | 層級 | 路徑 |
|------|------|------|
| `PlanNode` 等 domain types | Domain | `internal/core/domain/plan.go` |
| `PlanConverter` | Application | `internal/core/application/orchestrator/converter.go` |
| `Orchestrator` | Application | `internal/core/application/orchestrator/orchestrator.go` |
| Plan / Evaluate response schema | Infrastructure | `internal/infra/llm/schema.go` |
| `prompts/plan.prompt` | Config | 專案根目錄 |
| `prompts/evaluate.prompt` | Config | 專案根目錄 |
| `prompts/respond.prompt` | Config | 專案根目錄 |

### 移除

| 元件 | 原因 |
|------|------|
| `agenttree.yaml` | 靜態 config 被動態 plan 取代 |
| `internal/infra/config/agenttree/` | YAML loader 不再需要 |
| `agents/root/agent.prompt` | Root 改為 Orchestrator 內部 LLM call |
| `agents/planner/agent.prompt` | 被 `prompts/plan.prompt` 取代 |
| `agents/executor/agent.prompt` | 動態生成 |
| `agents/reporter/agent.prompt` | 被 `prompts/respond.prompt` 取代 |
| `agents/worker/agent.prompt` | 動態生成 |
| `agents/evaluator/agent.prompt` | 被 `prompts/evaluate.prompt` 取代 |
| `wire.go` legacy / YAML 模式 | 不向後相容，只保留 Orchestrator 模式 |

### 保留

| 元件 | 原因 |
|------|------|
| `internal/core/domain/agenttree.go` | `AgentNodeConfig` 被 PlanConverter 復用 |
| `internal/core/application/agenttree/builder.go` | 核心 builder，小幅修改（加 callback 支援） |
| `agents/` 目錄結構 | 存放可選的 role template prompt |
| Memory plugin / Shell tool / MCP toolset | 掛到動態 agent 上 |
| Session / Persistence 層 | 不動 |

### wire.go 改造

移除 legacy 和 YAML 模式，直接建構 Orchestrator：

```
wire.go → Orchestrator{PlannerModel, Builder, Schemas, ...}
```

Entrypoints（`cmd/agent`、`cmd/telegram`、`cmd/web`）呼叫 `orchestrator.Run()` 而非 `runner.Run()`。

---

## ADK Runner 生命週期

動態編排下，每次 `orchestrator.Run()` 都建構不同的 agent tree，因此 ADK Runner 的管理方式改變：

### 每次呼叫建構新 Runner

```go
func (o *Orchestrator) execute(ctx context.Context, agentTree agent.Agent, sess *session.Session) (map[string]any, error) {
    r, err := runner.New(runner.Config{
        AppName:        o.AppName,
        Agent:          agentTree,       // 每次都是新建構的 agent tree
        SessionService: o.SessionService, // 共用，持久化跨 request
    })
    // ... run and collect results
}
```

- `runner.Runner`：每次 `execute()` 建新的（cheap — 只是包一層 agent reference）
- `session.Service`：整個 Orchestrator 共用一個（持久化 session state 跨 request）
- `session.Session`：每次 `orchestrator.Run()` 建新的，確保 state 隔離

### Session State 隔離

每次 `orchestrator.Run()` 建立一個新的 session ID（或使用 entrypoint 傳入的 session ID）。動態 agent 的 `output_key` 寫入此 session 的 state。不同 request 之間 state 不互相干擾。

外層 loop 的重試共用同一個 session：第二輪 plan 可以看到第一輪的 state，讓 feedback 驅動的重新規劃能參考之前的結果。

### Plugins

Memory plugin、debug plugin 等在 `wire.go` 初始化時注入 Orchestrator，Orchestrator 在每次建構 Runner 時傳入。Plugin 是無狀態的，可以安全跨 Runner 復用。

---

## 端到端流程範例

### 範例 1：複雜任務

使用者從 Telegram 發送：「幫我研究 Go 的 error handling best practices 並寫一份報告」

**Phase 1: Plan** — Root LLM 輸出：
```json
{
  "intent": "研究 Go error handling 並產出報告",
  "max_retries": 1,
  "plan": {
    "type": "sequential",
    "steps": [
      {
        "type": "step",
        "role": "researcher",
        "instruction": "搜尋 Go error handling 的主流做法：errors.Is/As、sentinel errors、error wrapping、自定義 error type。整理成結構化筆記。",
        "tools": ["web_search"],
        "output_key": "research"
      },
      {
        "type": "step",
        "role": "writer",
        "instruction": "根據 {research} 撰寫一份技術報告，包含：概述、各方法比較、推薦做法、程式碼範例。",
        "output_key": "report"
      }
    ]
  }
}
```

**Phase 2: Execute** — 系統建構 `SequentialAgent[researcher, writer]`，依序執行。

**Phase 3: Evaluate** — Root LLM 回傳 `{ "satisfied": true }`。

**Phase 4: Respond** — Root LLM 整理 `state["report"]` 成使用者友善回覆。

### 範例 2：簡單問答

使用者從 Telegram 發送：「Go 的 goroutine 是什麼？」

**Phase 1: Plan** — Root LLM 輸出：
```json
{
  "intent": "簡單概念解釋",
  "max_retries": 0,
  "plan": {
    "type": "direct",
    "response": "Goroutine 是 Go 的輕量級執行緒..."
  }
}
```

→ 快速路徑，直接回傳。

### 範例 3：迭代任務

使用者從 Telegram 發送：「幫我寫一個 binary search 函數，要通過所有邊界測試」

**Phase 1: Plan** — Root LLM 輸出含 `loop` 的 plan。

**Phase 2: Execute** — `LoopAgent[Sequential[coder, tester]]` 反覆執行直到 tester 通過或達 max_iterations。

**Phase 3: Evaluate** — Root LLM 檢查最終結果。若不滿意帶 feedback 回 Phase 1。

**Phase 4: Respond** — 整理程式碼 + 測試結果回覆使用者。
