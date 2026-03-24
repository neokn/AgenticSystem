# 動態計畫 Patterns 指南

> 本文件說明 AgenticSystem 的動態計畫結構，包含 Sequential、Loop、Parallel 三種節點類型的 JSON 表示、組合方式，以及 exit_condition 機制的運作原理。

---

## 目錄

1. [設計理念](#設計理念)
2. [PlanNode 結構](#plannode-結構)
3. [Pattern 1: Sequential（循序執行）](#pattern-1-sequential循序執行)
4. [Pattern 2: Loop（迭代執行）](#pattern-2-loop迭代執行)
5. [Pattern 3: Parallel（並行執行）](#pattern-3-parallel並行執行)
6. [嵌套組合](#嵌套組合)
7. [exit_condition 機制](#exit_condition-機制)
8. [output_key 流動設計](#output_key-流動設計)

---

## 設計理念

動態計畫 pattern 的核心原則：

1. **執行期生成**：計畫由 Planner LLM 在執行期根據使用者請求動態決定，不依賴靜態設定檔
2. **JSON 表示**：計畫是結構化 JSON，可由 LLM 生成，也可測試驗證
3. **State-driven**：Agent 之間透過 session state 溝通，output_key 決定資料流
4. **可組合**：三種 pattern 可以任意嵌套，形成複雜的執行計畫

---

## PlanNode 結構

所有計畫都是由 `PlanNode` 節點組成的樹狀結構：

```go
type PlanNode struct {
    Type PlanNodeType `json:"type"` // "direct" | "sequential" | "loop" | "parallel" | "step"

    // direct only
    Response string `json:"response,omitempty"`

    // sequential / loop / parallel
    Steps []PlanNode `json:"steps,omitempty"`

    // loop only
    MaxIterations uint           `json:"max_iterations,omitempty"`
    ExitCondition *ExitCondition `json:"exit_condition,omitempty"`

    // step only
    Role        string   `json:"role,omitempty"`
    Instruction string   `json:"instruction,omitempty"`
    Tools       []string `json:"tools,omitempty"`
    OutputKey   string   `json:"output_key,omitempty"`
}

type ExitCondition struct {
    OutputKey string `json:"output_key"`
    Pattern   string `json:"pattern"`
}
```

頂層 `PlanOutput` 包含計畫樹加上元資料：

```go
type PlanOutput struct {
    Intent     string   `json:"intent"`      // Planner 理解的使用者意圖
    MaxRetries int      `json:"max_retries"` // 允許的重試次數
    Plan       PlanNode `json:"plan"`        // 計畫樹根節點
}
```

### Step 節點欄位說明

| 欄位 | 說明 | 是否必填 |
|------|------|---------|
| `role` | agent role 名稱，對應 `agents/<role>/agent.prompt` | 是 |
| `instruction` | 這次執行的具體指示，會附加在 role prompt 後面 | 是 |
| `tools` | 允許使用的 tool 名稱列表（如 `shell_exec`）| 否 |
| `output_key` | 輸出寫入的 session state key 名稱 | 是 |

---

## Pattern 1: Sequential（循序執行）

### 說明

按 `steps` 列表順序依序執行每個子節點。前一個 step 的輸出會存入 session state，後續 step 透過 `{key?}` 語法讀取。

### JSON 結構

```json
{
  "type": "sequential",
  "steps": [
    {
      "type": "step",
      "role": "planner",
      "instruction": "分析需求，制定執行計畫",
      "output_key": "plan"
    },
    {
      "type": "step",
      "role": "executor",
      "instruction": "根據 {plan?} 執行任務",
      "tools": ["shell_exec"],
      "output_key": "artifacts"
    },
    {
      "type": "step",
      "role": "reporter",
      "instruction": "根據 {plan?} 和 {artifacts?} 整理成報告",
      "output_key": "summary"
    }
  ]
}
```

### 轉換後的 AgentNodeConfig

```
seq_0 (SequentialAgent)
  └─ planner_1   (LlmAgent, output_key: plan)
  └─ executor_2  (LlmAgent, tools: [shell_exec], output_key: artifacts)
  └─ reporter_3  (LlmAgent, output_key: summary)
```

### state 流動

```
planner_1 → state["plan"] → executor_2 → state["artifacts"] → reporter_3 → state["summary"]
```

### 適用場景

| 場景 | 範例請求 |
|------|---------|
| 技術調研報告 | 「幫我研究 GraphQL vs REST 的優缺點並寫份報告」 |
| 多步驟分析 | 「分析這個系統的效能瓶頸並提出三個改善方案」 |
| 文件撰寫 | 「幫我寫一份 API 設計文件」 |

---

## Pattern 2: Loop（迭代執行）

### 說明

重複執行 `steps` 中的節點，直到 `exit_condition` 成立或達到 `max_iterations` 上限。適合「做完就檢查、不滿意就修改」的任務。

### JSON 結構（無 exit_condition）

```json
{
  "type": "loop",
  "max_iterations": 5,
  "steps": [
    {
      "type": "step",
      "role": "worker",
      "instruction": "根據 {evaluation?} 的回饋改善草稿",
      "output_key": "draft"
    },
    {
      "type": "step",
      "role": "evaluator",
      "instruction": "評估 {draft?}，若達標說明通過原因，否則列出問題",
      "output_key": "evaluation"
    }
  ]
}
```

不設定 `exit_condition` 時，迴圈執行滿 `max_iterations` 次後結束。

### JSON 結構（含 exit_condition）

```json
{
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
      "instruction": "根據 {evaluation?} 修改程式碼",
      "tools": ["shell_exec"],
      "output_key": "draft"
    },
    {
      "type": "step",
      "role": "evaluator",
      "instruction": "執行測試，全部通過回覆 PASS，否則說明失敗原因",
      "tools": ["shell_exec"],
      "output_key": "evaluation"
    }
  ]
}
```

### 轉換後的 AgentNodeConfig

```
loop_0 (LoopAgent, max_iterations: 5)
  └─ seq_3 (SequentialAgent — body wrapper)
       └─ worker_1     (LlmAgent, tools: [shell_exec], output_key: draft)
       └─ evaluator_2  (LlmAgent, tools: [shell_exec], output_key: evaluation)
       └─ exit_checker_4  (sentinel, 監控 evaluation 是否含 "PASS")
```

`exit_checker` 是 `Convert()` 自動插入的 sentinel，不需要在計畫 JSON 中手動定義。

### state 流動（迭代）

```
每輪:
  worker_1 → state["draft"] → evaluator_2 → state["evaluation"] → exit_checker_4
                                                                        │
                                                       含 "PASS" → escalate（結束）
                                                       不含 "PASS" → 繼續下一輪
```

### 適用場景

| 場景 | 範例請求 |
|------|---------|
| 程式碼撰寫 + 驗證 | 「幫我寫一個排序函數，確保通過所有邊界測試」 |
| 文字潤飾 | 「幫我把這段文字改到專業水準」 |
| 反覆修正 | 「幫我修正這段 SQL query 直到結果正確」 |

---

## Pattern 3: Parallel（並行執行）

### 說明

同時執行所有 `steps` 節點，各自在獨立的 session 分支中運行。適合需要多角度分析、不互相依賴的任務。

### JSON 結構

```json
{
  "type": "parallel",
  "steps": [
    {
      "type": "step",
      "role": "executor",
      "instruction": "從安全角度審查程式碼",
      "output_key": "security_review"
    },
    {
      "type": "step",
      "role": "executor",
      "instruction": "從效能角度審查程式碼",
      "output_key": "performance_review"
    },
    {
      "type": "step",
      "role": "executor",
      "instruction": "從可維護性角度審查程式碼",
      "output_key": "maintainability_review"
    }
  ]
}
```

### 轉換後的 AgentNodeConfig

```
par_0 (ParallelAgent)
  └─ executor_1  (LlmAgent, output_key: security_review)
  └─ executor_2  (LlmAgent, output_key: performance_review)
  └─ executor_3  (LlmAgent, output_key: maintainability_review)
```

### state 流動

三個 step 並行執行，各自寫入不同的 output_key，不互相干擾：

```
par_0 ──┬── executor_1 → state["security_review"]
        ├── executor_2 → state["performance_review"]
        └── executor_3 → state["maintainability_review"]
```

### 適用場景

| 場景 | 範例請求 |
|------|---------|
| 多角度審查 | 「幫我從安全、效能、可維護性三個角度審查這段程式碼」 |
| 多方案生成 | 「給我三個不同的架構方案」 |
| 獨立子任務 | 「同時整理前三季的銷售數據」 |

---

## 嵌套組合

三種 pattern 可以任意嵌套，形成複雜的執行計畫。

### Sequential 嵌套 Loop（規劃 + 迭代 + 報告）

```json
{
  "type": "sequential",
  "steps": [
    {
      "type": "step",
      "role": "planner",
      "instruction": "制定重構計畫",
      "output_key": "plan"
    },
    {
      "type": "loop",
      "max_iterations": 5,
      "exit_condition": { "output_key": "evaluation", "pattern": "ALL_PASS" },
      "steps": [
        {
          "type": "step",
          "role": "worker",
          "instruction": "根據 {plan?} 執行重構，根據 {evaluation?} 修正",
          "tools": ["shell_exec"],
          "output_key": "draft"
        },
        {
          "type": "step",
          "role": "evaluator",
          "instruction": "執行測試，全部通過回覆 ALL_PASS",
          "tools": ["shell_exec"],
          "output_key": "evaluation"
        }
      ]
    },
    {
      "type": "step",
      "role": "reporter",
      "instruction": "根據 {plan?} 和最終 {draft?} 產出重構報告",
      "output_key": "summary"
    }
  ]
}
```

### Sequential 嵌套 Parallel（研究 + 多角度整理 + 報告）

```json
{
  "type": "sequential",
  "steps": [
    {
      "type": "step",
      "role": "planner",
      "instruction": "確定調研範圍",
      "output_key": "plan"
    },
    {
      "type": "parallel",
      "steps": [
        {
          "type": "step",
          "role": "executor",
          "instruction": "調研技術面",
          "output_key": "tech_research"
        },
        {
          "type": "step",
          "role": "executor",
          "instruction": "調研市場面",
          "output_key": "market_research"
        }
      ]
    },
    {
      "type": "step",
      "role": "reporter",
      "instruction": "彙整 {tech_research?} 和 {market_research?} 產出完整報告",
      "output_key": "summary"
    }
  ]
}
```

### 轉換後的樹狀結構

```
seq_0
  └─ planner_1     (LlmAgent)
  └─ par_2         (ParallelAgent)
  │    └─ executor_3  (LlmAgent, output_key: tech_research)
  │    └─ executor_4  (LlmAgent, output_key: market_research)
  └─ reporter_5    (LlmAgent)
```

---

## exit_condition 機制

### 運作原理

`exit_condition` 讓 Loop 在品質達標時提前結束，而不必跑完所有 `max_iterations`。

Planner 在計畫中指定：
- `output_key`：要監控哪個 state key
- `pattern`：當該 key 的值包含此子字串時，結束迴圈

`Convert()` 在轉換 loop 節點時，自動在每輪迭代的最後插入一個 `exit_checker` sentinel agent：

```go
// converter.go — 自動插入 exit_checker
if node.ExitCondition != nil {
    exitChecker := domain.AgentNodeConfig{
        Name:        "exit_checker_" + itoa(checkerIdx),
        Type:        domain.AgentTypeLLM,
        Instruction: "__EXIT_CHECKER__",
        OutputKey:   node.ExitCondition.OutputKey + "|" + node.ExitCondition.Pattern,
    }
    bodySubAgents = append(bodySubAgents, exitChecker)
}
```

### exit_checker 邏輯

`exitchecker.go` 的 `NewExitChecker` 建立一個輕量 sentinel agent：

```
每輪結束前：
  1. 讀取 session state[OutputKey]
  2. 檢查值是否包含 Pattern（子字串，大小寫敏感）
  3. 若包含 → 發出 event.Actions.Escalate = true → LoopAgent 停止
  4. 若不包含 → 不發出 event → LoopAgent 繼續下一輪
```

### Pattern 設計建議

| Pattern 設計 | 建議 | 說明 |
|-------------|------|------|
| 獨特性 | `"PASS"` 比 `"ok"` 好 | 避免與正常輸出混淆 |
| 大小寫 | 固定大寫 | exit_checker 是大小寫敏感比對 |
| 語義清晰 | `"ALL_PASS"` 或 `"APPROVED"` | evaluator prompt 和 pattern 需一致 |

### 迴圈結束條件總覽

| 條件 | 說明 |
|------|------|
| exit_checker 發出 escalate | `exit_condition.pattern` 在指定 state key 中被偵測到 |
| max_iterations 達到 | 安全上限，防止無限迴圈 |
| ADK context window 耗盡 | ADK 內建保護機制 |

建議永遠設定合理的 `max_iterations`（3–7），即使有 `exit_condition` 也一樣，作為防護底線。

---

## output_key 流動設計

### 設計原則

- 每個 step 節點有且只有一個 `output_key`（唯一寫入者）
- 後續 step 透過 `{key?}` 語法讀取（`?` 表示 optional，首次迭代 key 不存在不報錯）
- loop 中每輪迭代的 step 會覆寫相同的 key（如 `draft`、`evaluation`）

### 常見 output_key 慣例

| output_key | 通常用於 | 通常由誰讀取 |
|-----------|---------|-----------|
| `plan` | planner role 的輸出 | executor / worker role |
| `artifacts` | executor role 的產出 | reporter role |
| `draft` | worker role 的草稿 | evaluator role |
| `evaluation` | evaluator role 的評估 | worker role (下一輪) + exit_checker |
| `summary` | reporter role 的最終摘要 | Responder (Phase 4) |

### 自訂 output_key

Planner 可以自由定義 output_key 名稱，只要保持一致即可：

```json
{
  "type": "sequential",
  "steps": [
    {
      "type": "step",
      "role": "executor",
      "instruction": "分析程式碼架構",
      "output_key": "code_analysis"
    },
    {
      "type": "step",
      "role": "executor",
      "instruction": "根據 {code_analysis?} 識別重構機會",
      "output_key": "refactor_opportunities"
    },
    {
      "type": "step",
      "role": "reporter",
      "instruction": "根據 {refactor_opportunities?} 產出重構建議",
      "output_key": "recommendations"
    }
  ]
}
```

重要原則：output_key 名稱在計畫 JSON 中的 `output_key` 和後續 step instruction 的 `{key?}` 佔位符必須完全一致。
