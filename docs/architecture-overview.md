# 架構總覽：動態協作編排系統

> 本文件描述 AgenticSystem 的完整架構設計，涵蓋 Orchestrator 四階段運作方式、元件職責分工，以及 Explicit Architecture 的層級結構。

---

## 目錄

1. [設計理念](#設計理念)
2. [Orchestrator 四階段架構](#orchestrator-四階段架構)
3. [Orchestrator 元件](#orchestrator-元件)
4. [動態計畫結構](#動態計畫結構)
5. [元件關係圖](#元件關係圖)
6. [Session State 通訊機制](#session-state-通訊機制)
7. [Loop 結束機制](#loop-結束機制)
8. [目錄結構對照](#目錄結構對照)

---

## 設計理念

本系統基於 Google ADK (Agent Development Kit) v1.0.0 建構，採用以下核心原則：

1. **動態計畫生成**：執行計畫由 Root LLM 在執行期根據使用者請求動態生成，而非靜態設定檔描述
2. **純函數式 Agent**：每個 agent 像純函數 — 讀 state、做事、寫 state，透過 session state 溝通
3. **通用執行骨架**：Orchestrator 不綁特定 domain，換 prompt 就能處理各類任務
4. **Explicit Architecture**：遵循 Hexagonal / Ports & Adapters 架構，依賴方向永遠向內

---

## Orchestrator 四階段架構

Orchestrator 是系統的核心協調者，每次收到使用者請求時依序執行四個階段：

```
使用者請求
    │
    ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Orchestrator                              │
│                                                                 │
│  Phase 1: Plan                                                  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Planner (Root LLM)                                       │  │
│  │  輸入：userPrompt + feedback (重試時) + availableTools    │  │
│  │  輸出：PlanOutput { intent, max_retries, plan: PlanNode } │  │
│  └────────────────────────┬─────────────────────────────────┘  │
│                           │                                     │
│                    direct │ sequential / loop / parallel        │
│                     ┌─────┘                                     │
│                     ▼                                           │
│              Phase 1 短路回傳                                   │
│              (Response 直接帶在 PlanOutput 中)                  │
│                                                                 │
│  Phase 2: Execute                                               │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Executor                                                 │  │
│  │  Convert(PlanNode) → AgentNodeConfig                      │  │
│  │  Build ADK agent tree → Runner.Run()                      │  │
│  │  輸出：results map[string]any (session state)             │  │
│  └────────────────────────┬─────────────────────────────────┘  │
│                           │                                     │
│  Phase 3: Evaluate                                              │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Evaluator (Root LLM)                                     │  │
│  │  輸入：userPrompt + results                               │  │
│  │  輸出：EvalOutput { satisfied bool, feedback string }     │  │
│  └────────────────────────┬─────────────────────────────────┘  │
│                           │                                     │
│               satisfied? │ No → feedback → Phase 1 (重試)      │
│                     Yes  │                                      │
│                     ▼                                           │
│  Phase 4: Respond                                               │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Responder (Root LLM)                                     │  │
│  │  輸入：userPrompt + results                               │  │
│  │  輸出：最終使用者友善回覆 (string)                         │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
    │
    ▼
最終回覆 → Telegram Bot / CLI / Web
```

### 重試迴圈

Phase 3 判定 `satisfied = false` 時，Orchestrator 將 `feedback` 帶回 Phase 1，讓 Planner 重新生成計畫。重試次數受兩個上限約束：

- `plan.MaxRetries`：Planner 自行評估這個任務需要的重試上限
- `Config.SystemMaxRetry`：系統層級的硬上限（預設 3）

取兩者較小值作為有效上限，防止無限重試。

---

## Orchestrator 元件

Orchestrator 依賴四個 port interface，具體實作由 infrastructure layer 注入：

### Planner

```go
type Planner interface {
    Plan(ctx context.Context,
         userPrompt, feedback string,
         availableTools, availableRoles []string) (*domain.PlanOutput, error)
}
```

- **職責**：分析使用者請求，決定執行策略，生成結構化計畫
- **實作**：`infra/llm.GeminiPlanner` — 使用 Gemini 結構化 JSON 輸出 + PlanSchema
- **計畫類型**：`direct`（直接回答）、`sequential`、`loop`、`parallel`

### Evaluator

```go
type Evaluator interface {
    Evaluate(ctx context.Context,
             userPrompt string,
             results map[string]any) (*domain.EvalOutput, error)
}
```

- **職責**：對照原始請求評估執行結果，決定是否滿意
- **實作**：`infra/llm.GeminiEvaluator` — 使用 Gemini 結構化 JSON 輸出
- **輸出**：`{ satisfied bool, feedback string }`

### Responder

```go
type Responder interface {
    Respond(ctx context.Context,
            userPrompt string,
            results map[string]any) (string, error)
}
```

- **職責**：將執行結果格式化為使用者友善的回覆
- **實作**：`infra/llm.GeminiResponder` — 使用 Gemini 自由文字輸出
- **時機**：只在 Phase 3 判定 `satisfied = true` 後呼叫

### Executor

```go
type Executor interface {
    Execute(ctx context.Context,
            cfg *domain.AgentNodeConfig) (map[string]any, error)
}
```

- **職責**：根據 AgentNodeConfig 建構並執行 ADK agent tree，回傳 session state
- **實作**：`infra/executor.ADKExecutor`
- **流程**：Convert(PlanNode) → Build(AgentNodeConfig) → Runner.Run → 讀取 session state

---

## 動態計畫結構

Planner 輸出的計畫是一棵 `PlanNode` 樹，由 Executor 的 `Convert()` 轉換為 ADK agent tree。

### PlanNode 類型

| 類型 | 說明 | 子節點 |
|------|------|--------|
| `direct` | 直接回覆，不執行 agent tree | 無 |
| `sequential` | 依序執行所有子節點 | `steps[]` |
| `loop` | 重複執行子節點直到退出條件成立 | `steps[]` + `exit_condition?` |
| `parallel` | 並行執行所有子節點 | `steps[]` |
| `step` | 末端執行節點，對應一個 LlmAgent | 無 |

### 計畫 JSON 範例

**直接回答（Phase 1 短路）：**
```json
{
  "intent": "簡單問答",
  "max_retries": 0,
  "plan": {
    "type": "direct",
    "response": "今天是 2026-03-25。"
  }
}
```

**循序執行計畫：**
```json
{
  "intent": "技術調研報告",
  "max_retries": 2,
  "plan": {
    "type": "sequential",
    "steps": [
      {
        "type": "step",
        "role": "planner",
        "instruction": "分析 WebSocket 與 SSE 的差異，列出比較維度",
        "output_key": "plan"
      },
      {
        "type": "step",
        "role": "executor",
        "instruction": "根據 {plan?} 執行調研，整理各維度資料",
        "tools": ["shell_exec"],
        "output_key": "artifacts"
      },
      {
        "type": "step",
        "role": "reporter",
        "instruction": "根據 {plan?} 和 {artifacts?} 撰寫比較報告",
        "output_key": "summary"
      }
    ]
  }
}
```

**迭代執行計畫（含 exit_condition）：**
```json
{
  "intent": "迭代改善程式碼直到通過測試",
  "max_retries": 1,
  "plan": {
    "type": "loop",
    "max_iterations": 5,
    "exit_condition": {
      "output_key": "evaluation",
      "pattern": "PASS"
    },
    "steps": [
      {
        "type": "step",
        "role": "worker",
        "instruction": "根據 {evaluation?} 的回饋修改程式碼",
        "tools": ["shell_exec"],
        "output_key": "draft"
      },
      {
        "type": "step",
        "role": "evaluator",
        "instruction": "執行測試，若全部通過回覆 PASS，否則說明問題",
        "tools": ["shell_exec"],
        "output_key": "evaluation"
      }
    ]
  }
}
```

### Convert 轉換規則

`orchestrator.Convert()` 遞迴將 PlanNode 轉換為 AgentNodeConfig：

| PlanNode 類型 | AgentNodeConfig 類型 | 名稱規則 |
|--------------|---------------------|---------|
| `step` | `llm` | `<role>_<index>` |
| `sequential` | `sequential` | `seq_<index>` |
| `loop` | `loop` + 內部 sequential wrapper | `loop_<index>` |
| `parallel` | `parallel` | `par_<index>` |

Loop 節點若有 `exit_condition`，會在 body 末尾自動插入 `exit_checker_<index>` sentinel agent。

---

## 元件關係圖

```
                ┌──────────────────────────────────────┐
                │           cmd/ 觸發層                  │
                │  telegram/  │  agent/  │  web/        │
                └──────┬──────┴────┬─────┴──────┬──────┘
                       │           │            │
                       ▼           ▼            ▼
                ┌──────────────────────────────────────┐
                │     internal/core/application/        │
                │                                      │
                │  wire.go (New → 組裝 App)             │
                │    │                                  │
                │    ├─ orchestrator.New(Config{...})   │
                │    │    ├─ Planner (GeminiPlanner)    │
                │    │    ├─ Evaluator (GeminiEvaluator)│
                │    │    ├─ Responder (GeminiResponder)│
                │    │    └─ Executor (ADKExecutor)     │
                │    │         └─ agenttree.Build()     │
                │    │                                  │
                │    ├─ SessionService                  │
                │    └─ MemoryPlugin                    │
                └──────────────┬───────────────────────┘
                               │
          ┌────────────────────┼────────────────────┐
          │                    │                    │
          ▼                    ▼                    ▼
┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│  core/domain │    │  core/port   │    │  infra/      │
│              │    │              │    │              │
│ PlanNode     │    │ Planner      │    │ llm/         │
│ AgentNode    │◄───│ Evaluator    │◄───│   GeminiP/E/R│
│ ExitCond     │    │ Responder    │    │ executor/    │
│ EvalOutput   │    │ Executor     │    │   ADKExecutor│
│ PlanOutput   │    │              │    │ config/      │
└──────────────┘    └──────────────┘    │   agentdef/  │
                                        │   mcpconfig/ │
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
application.New() → 載入 prompts/ (plan/evaluate/respond)
    │              → 掃描 agents/ (available roles)
    │              → 建立 GeminiPlanner / GeminiEvaluator / GeminiResponder
    │              → 建立 ADKExecutor (含 agenttree.Build 相依)
    │
    ▼
orchestrator.Run(userPrompt)
    │
    ├─ Phase 1: Planner.Plan() → PlanOutput (JSON)
    │      │
    │      ├─ direct → 立即回傳 Response
    │      └─ 非 direct → Phase 2
    │
    ├─ Phase 2: Convert(PlanNode) → AgentNodeConfig
    │           Executor.Execute() → 建構 ADK agent tree, 執行, 回傳 session state
    │
    ├─ Phase 3: Evaluator.Evaluate() → EvalOutput
    │      │
    │      ├─ satisfied=false → feedback → 回到 Phase 1
    │      └─ satisfied=true → Phase 4
    │
    └─ Phase 4: Responder.Respond() → 最終回覆 string
    │
    ▼
cmd/telegram → 發送到 Telegram
```

---

## Session State 通訊機制

Agent tree 執行期間，各 LlmAgent 透過 session state 溝通。每個 step 節點設定 `output_key`，其文字輸出自動存入該 key。後續 step 透過 prompt 中的 `{key?}` 語法讀取。

### 常見 State Keys（由計畫動態決定）

計畫中每個 step 的 `output_key` 欄位決定要寫入哪個 state key。以下是常見慣例：

| Key | 通常由誰寫入 | 通常由誰讀取 | 用途 |
|-----|-------------|-------------|------|
| `plan` | planner role | executor role | 結構化執行計畫 |
| `artifacts` | executor role | reporter role | 執行步驟產出 |
| `draft` | worker role | evaluator role | 迭代中的工作草稿 |
| `evaluation` | evaluator role | worker role | 評估回饋 |
| `summary` | reporter role | Responder | 最終摘要 |

### output_key 流動原則

- `output_key` 是 step 節點的屬性，在計畫生成時由 Planner 決定
- Responder 收到的 `results` 就是整個 agent tree 執行後的 session state
- `{key?}` 的 `?` 後綴表示 optional — 首次迭代時 key 不存在不會報錯

---

## Loop 結束機制

Loop 節點在以下條件成立時結束：

1. **Exit Checker Sentinel**：若計畫的 loop 節點設定了 `exit_condition`，`Convert()` 會自動在每輪迭代末尾插入一個 `exit_checker` sentinel agent。
   - sentinel 讀取指定的 `output_key` 狀態值
   - 若值包含指定的 `pattern`（子字串比對，區分大小寫），發出 `escalate = true` 事件
   - ADK LoopAgent 收到 escalate 事件後終止迴圈

2. **max_iterations 達到**：安全上限，防止無限迴圈

3. **context window 耗盡**：ADK 內建安全機制

```
每輪迭代：
    step_0 (worker)
        │
        ▼
    step_1 (evaluator) → 寫入 state["evaluation"]
        │
        ▼
    exit_checker → 讀取 state["evaluation"]
                → 若包含 "PASS" → escalate → 迴圈結束
                → 若不包含 → 繼續下一輪
```

---

## 目錄結構對照

```
AgenticSystem/
├── prompts/                                # Orchestrator 核心 prompt 檔
│   ├── plan.prompt                         #   Planner 系統指令 (含佔位符)
│   ├── evaluate.prompt                     #   Evaluator 系統指令
│   ├── respond.prompt                      #   Responder 系統指令
│   └── summarize.prompt                    #   (其他輔助 prompt)
├── agents/                                 # Agent role 定義 (資料，非程式碼)
│   ├── planner/agent.prompt                #   planner role 的 prompt 模板
│   ├── executor/agent.prompt               #   executor role 的 prompt 模板
│   ├── reporter/agent.prompt               #   reporter role 的 prompt 模板
│   ├── worker/agent.prompt                 #   worker role 的 prompt 模板
│   └── evaluator/agent.prompt              #   evaluator role 的 prompt 模板
├── internal/
│   ├── core/
│   │   ├── domain/
│   │   │   ├── plan.go                     #   PlanNode / PlanOutput / EvalOutput
│   │   │   ├── agenttree.go                #   AgentNodeConfig / AgentTreeConfig
│   │   │   └── model.go                    #   其他 domain types
│   │   ├── port/
│   │   │   ├── agent.go                    #   AgentLoader port
│   │   │   └── tool.go                     #   ToolProvider port
│   │   └── application/
│   │       ├── wire.go                     #   App 組裝入口
│   │       ├── orchestrator/
│   │       │   ├── orchestrator.go         #   四階段 Orchestrator + Planner/Evaluator/Responder/Executor ports
│   │       │   ├── converter.go            #   PlanNode → AgentNodeConfig 轉換
│   │       │   └── exitchecker.go          #   Loop exit condition sentinel agent
│   │       └── agenttree/
│   │           └── builder.go              #   AgentNodeConfig → ADK agent tree 建構
│   └── infra/
│       ├── llm/
│       │   ├── planner.go                  #   GeminiPlanner (Planner port 實作)
│       │   ├── evaluator.go                #   GeminiEvaluator (Evaluator port 實作)
│       │   ├── responder.go                #   GeminiResponder (Responder port 實作)
│       │   ├── helpers.go                  #   buildPlanSystemInstruction / parsePlanOutput 等
│       │   └── schema.go                   #   PlanSchema() JSON schema for structured output
│       ├── executor/
│       │   └── executor.go                 #   ADKExecutor (Executor port 實作)
│       └── config/
│           ├── agentdef/                   #   dotprompt loader
│           └── mcpconfig/                  #   MCP config loader
└── docs/
    ├── architecture-overview.md            #   本文件
    ├── end-to-end-flow.md                  #   端到端流程範例
    └── workflow-patterns-guide.md          #   動態計畫 pattern 指南
```
