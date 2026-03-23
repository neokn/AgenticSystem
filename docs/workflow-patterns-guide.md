# Workflow Patterns 指南

> 本文件說明 AgenticSystem 提供的兩種核心 workflow pattern 的設計理念、state 流動方式，以及使用方式。

---

## 目錄

1. [設計理念](#設計理念)
2. [State Keys 規範](#state-keys-規範)
3. [Pattern 1: Plan-Execute-Report](#pattern-1-plan-execute-report)
4. [Pattern 2: Iterative Refinement](#pattern-2-iterative-refinement)
5. [混合組合：Plan-Iterate-Report](#混合組合plan-iterate-report)
6. [Prompt Template 設計](#prompt-template-設計)
7. [迴圈結束條件](#迴圈結束條件)
8. [自訂 Workflow](#自訂-workflow)

---

## 設計理念

Workflow pattern 的核心設計原則：

1. **通用骨架**：Pattern 本身不綁任何 domain，換 prompt 就變成不同用途
2. **State-driven**：Agent 之間透過 session state 溝通，不透過直接呼叫
3. **可組合**：兩個 pattern 可以任意巢套，形成更複雜的 workflow
4. **可觀測**：每個 agent 的輸出都存入可查詢的 state key

### 為什麼不用單一 LlmAgent？

對於簡單任務，Root Agent 自己處理就夠了。但複雜任務有以下問題：

- **Token 限制**：一次 LLM call 很難完成需要多步驟的任務
- **品質控制**：沒有「做完檢查」的機制，容易出錯
- **可追蹤性**：一大段回覆很難知道哪一步出了問題
- **可重試性**：如果某一步失敗，無法只重試該步驟

Workflow pattern 透過拆分職責（規劃、執行、評估、報告）解決這些問題。

---

## State Keys 規範

所有 workflow pattern 共用以下標準 state keys：

| Key | 類型 | 寫入者 | 讀取者 | 生命週期 |
|-----|------|--------|--------|---------|
| `user_intent` | string | Root Agent | Planner, Worker | 整個 workflow |
| `plan` | string (Markdown) | Planner | Executor, Worker, Evaluator | 整個 workflow |
| `artifacts` | string (Markdown) | Executor | Reporter | 整個 workflow |
| `draft` | string | Worker | Evaluator | 每次迭代覆寫 |
| `evaluation` | string (Markdown) | Evaluator | Worker | 每次迭代覆寫 |
| `summary` | string (Markdown) | Reporter | Root Agent | workflow 結束時 |

### State 命名慣例

- 使用 snake_case
- 語義清晰，不加 prefix（不是 `workflow_plan`，而是 `plan`）
- 每個 key 只有一個 canonical writer
- Reader 透過 ADK 的 `{key?}` 語法讀取（`?` 表示 optional，首次迭代時不存在不會報錯）

---

## Pattern 1: Plan-Execute-Report

### 結構

```
SequentialAgent (plan_execute_report)
  ├─ Planner   (LlmAgent, output_key: plan)
  ├─ Executor  (LlmAgent, output_key: artifacts)
  └─ Reporter  (LlmAgent, output_key: summary)
```

### State 流動

```
┌────────┐    plan    ┌──────────┐  artifacts  ┌──────────┐  summary
│Planner │ ─────────► │ Executor │ ──────────► │ Reporter │ ────────►
└────────┘            └──────────┘             └──────────┘
     ▲                     │                        │
     │                     │ 可使用 tools            │ escalate
  user_intent              │ (shell_exec 等)        │ 回到 Root
```

### 適用場景

| 場景 | 範例 |
|------|------|
| 技術調研 | 「幫我研究 GraphQL vs REST 的優缺點並寫份報告」 |
| 程式重構 | 「幫我把這個模組從 callback 改成 async/await」 |
| 文件撰寫 | 「幫我寫一份 API 設計文件」 |
| 多步驟分析 | 「分析這個系統的效能瓶頸並提出三個改善方案」 |

### 各 Agent 的角色

#### Planner
- **輸入**：使用者的原始需求
- **輸出**：結構化的執行計畫（步驟、驗收標準、風險）
- **原則**：步驟要具體可執行，不超過 7 步

#### Executor
- **輸入**：`{plan?}` 中的計畫
- **輸出**：每個步驟的執行結果和產出
- **原則**：忠實執行計畫，遇到問題記錄但繼續

#### Reporter
- **輸入**：`{plan?}` 和 `{artifacts?}`
- **輸出**：使用者友善的最終報告
- **原則**：站在使用者角度撰寫，誠實報告失敗

---

## Pattern 2: Iterative Refinement

### 結構

```
LoopAgent (iterative_refinement, max_iterations: 5)
  └─ SequentialAgent (refinement_step)
       ├─ Worker    (LlmAgent, output_key: draft)
       └─ Evaluator (LlmAgent, output_key: evaluation)
```

### State 流動

```
 ┌─────────────────────────────────────────────────────┐
 │                  LoopAgent                           │
 │                                                     │
 │  ┌────────┐   draft   ┌───────────┐  evaluation     │
 │  │ Worker │ ────────► │ Evaluator │ ─────┐          │
 │  └────────┘           └───────────┘      │          │
 │       ▲                                  │          │
 │       │              ┌───────────────────┘          │
 │       │              │                              │
 │       │         ┌────┴─────┐                        │
 │       │         │ 達標？    │                        │
 │       │         ├──────────┤                        │
 │       │    否   │ 繼續迴圈  │───── evaluation ──────┘
 │       └─────────┤          │      (修改建議)
 │                 ├──────────┤
 │            是   │ escalate │── 結束迴圈
 │                 └──────────┘
 │                                                     │
 └─────────────────────────────────────────────────────┘
```

### 適用場景

| 場景 | 範例 |
|------|------|
| 寫程式碼 + 測試 | 「幫我寫一個排序函數，要確保單元測試通過」 |
| 文字潤飾 | 「幫我把這段文字改到專業水準」 |
| 反覆修正 | 「幫我修正這段 SQL query 直到結果正確」 |
| 產出 + 驗證 | 任何「做完要檢查」的任務 |

### 各 Agent 的角色

#### Worker
- **首次迭代**：根據需求產出第一版草稿
- **後續迭代**：讀取 `{evaluation?}` 的回饋，修改草稿
- **輸出**：工作草稿，存入 `draft`
- **原則**：針對回饋修改，不推翻好的部分

#### Evaluator
- **輸入**：`{draft?}` 中的最新草稿
- **輸出**：評估結果，存入 `evaluation`
- **達標時**：escalate 結束迴圈
- **未達標時**：列出問題和修改建議，不 escalate

### 迴圈結束條件

迴圈在以下任一條件達成時結束：

1. **Evaluator escalate**：品質達標，評估者主動結束
2. **max_iterations 達到**：安全上限，防止無限迴圈
3. **context window 耗盡**：ADK 內建的安全機制

建議總是設定合理的 `max_iterations`（推薦 3-5），避免浪費 token。

---

## 混合組合：Plan-Iterate-Report

### 結構

```
SequentialAgent (plan_iterate_report)
  ├─ Planner      (LlmAgent, output_key: plan)
  ├─ LoopAgent    (iterate_execute, max_iterations: 5)
  │   └─ SequentialAgent (iterate_step)
  │        ├─ Worker    (LlmAgent, output_key: draft)
  │        └─ Evaluator (LlmAgent, output_key: evaluation)
  └─ Reporter     (LlmAgent, output_key: summary)
```

### State 流動

```
Planner ──plan──► LoopAgent ┐
                            │
              ┌─────────────┘
              │
              │  Worker ──draft──► Evaluator ──evaluation──►
              │    ▲                    │
              │    └────────────────────┘  (迴圈)
              │                    │
              │              escalate 結束
              │                    │
              └────────────────────┘
                                   │
                            Reporter ──summary──► Root
```

### 適用場景

| 場景 | 範例 |
|------|------|
| 帶測試的重構 | 「幫我重構這個模組並確保所有測試通過」 |
| 完整功能開發 | 「幫我實作排序功能，包含程式碼和測試」 |
| 優化 + 報告 | 「幫我優化這個演算法的效能並寫測試報告」 |

---

## Prompt Template 設計

每個 workflow agent 的 prompt 都遵循統一的結構：

```
# 你是 <角色名> -- <一句話描述>

## 職責
（這個 agent 要做什麼）

## 輸入來源
（從哪些 state key 讀取資料）

## 輸出格式
（輸出的格式和存入的 state key）

## 原則
（工作原則和限制）
```

### Prompt 中的 State 變數

ADK 支援在 instruction 中使用 `{key}` 語法注入 session state：

```
你可以從以下 state 讀取資訊：
- 計畫：{plan?}
- 上一版草稿：{draft?}
- 評估回饋：{evaluation?}
```

`?` 後綴表示 optional — 如果 key 不存在不會報錯（首次迭代時有用）。

### 通用性設計

所有 prompt 都是 domain-agnostic（不綁特定領域）。
同一組 workflow 可以處理不同類型的任務：

- 「寫一份技術報告」→ Planner 規劃報告結構，Executor 撰寫各章節
- 「重構一段程式碼」→ Planner 規劃重構步驟，Executor 執行程式碼修改
- 「優化演算法效能」→ Planner 分析瓶頸，Executor 實施優化

差異完全來自使用者的需求描述，workflow 的骨架不變。

---

## 自訂 Workflow

你可以在 `agenttree.yaml` 中自由定義新的 workflow pattern。
只要遵循 ADK agent types 的組合規則，任何結構都是合法的。

### 範例：Parallel Review（多人審查）

```yaml
- name: parallel_review
  type: parallel
  description: "Multiple reviewers examine the draft in parallel."
  sub_agents:
    - name: security_reviewer
      type: llm
      description: "Reviews for security vulnerabilities."
      output_key: security_review
    - name: performance_reviewer
      type: llm
      description: "Reviews for performance issues."
      output_key: performance_review
```

### 範例：Multi-stage Pipeline

```yaml
- name: pipeline
  type: sequential
  sub_agents:
    - name: stage1_research
      type: llm
      output_key: research
    - name: stage2_draft
      type: llm
      output_key: draft
    - name: stage3_review
      type: parallel
      sub_agents:
        - name: tech_review
          type: llm
          output_key: tech_review
        - name: style_review
          type: llm
          output_key: style_review
    - name: stage4_final
      type: llm
      output_key: final
```
