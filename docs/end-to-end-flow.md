# 端到端流程範例

> 本文件以四個具體範例說明使用者訊息從 Telegram 進入系統、經過 Orchestrator 四階段處理、到最終回覆的完整流程。

---

## 目錄

1. [系統流程概覽](#系統流程概覽)
2. [範例一：簡單問答（Phase 1 直接回傳）](#範例一簡單問答phase-1-直接回傳)
3. [範例二：複雜任務（循序執行）](#範例二複雜任務循序執行)
4. [範例三：迭代任務（Loop 執行）](#範例三迭代任務loop-執行)
5. [範例四：混合任務（嵌套計畫）](#範例四混合任務嵌套計畫)
6. [完整元件互動圖](#完整元件互動圖)

---

## 系統流程概覽

所有請求都遵循相同的入口流程：

```
使用者 (Telegram)
    │ 發送訊息
    ▼
cmd/telegram/main.go
    │ 建立 session, 呼叫 orchestrator.Run(userPrompt)
    ▼
Orchestrator.Run()
    │
    ├─ Phase 1: Planner → PlanOutput
    │      direct → 立即回傳
    │      非 direct → Phase 2
    │
    ├─ Phase 2: Executor → 執行 ADK agent tree → session state (results)
    │
    ├─ Phase 3: Evaluator → EvalOutput
    │      satisfied=false → feedback → 回到 Phase 1
    │      satisfied=true → Phase 4
    │
    └─ Phase 4: Responder → 最終回覆 string
    │
    ▼
cmd/telegram/main.go
    │ 將回覆文字發送給使用者
    ▼
使用者 (Telegram) 收到回覆
```

---

## 範例一：簡單問答（Phase 1 直接回傳）

> 使用者：「今天天氣如何？」

### 流程

```
使用者 ─── 「今天天氣如何？」 ──► Telegram Bot

Telegram Bot:
  1. 取得 session (tg-<chatID>)
  2. 呼叫 orchestrator.Run("今天天氣如何？")

Phase 1 — Planner:
  3. GeminiPlanner.Plan() 呼叫 Gemini API
  4. Planner 判斷：這是簡單問答，不需要執行 agent tree
  5. 生成 direct 計畫：
     {
       "intent": "簡單天氣問答",
       "max_retries": 0,
       "plan": {
         "type": "direct",
         "response": "我沒有即時天氣資料，建議查看中央氣象署網站..."
       }
     }

Orchestrator:
  6. 偵測到 plan.Type == "direct"
  7. 立即回傳 Result{Response: "...", IsDirect: true}
     (不進入 Phase 2、3、4)

Telegram Bot:
  8. 將回覆文字發送給使用者
```

### 為什麼使用 direct 類型？

Planner 的 `plan.prompt` 定義了路由規則。簡單問答、閒聊、單步查詢都歸 `direct` 類型處理。不需要執行 agent tree 的任務，直接帶回答在計畫中是最省 token 的做法。

### session state 變化

```
(無 agent tree 執行 — direct 計畫不觸發 Phase 2)
```

---

## 範例二：複雜任務（循序執行）

> 使用者：「幫我研究 WebSocket 和 SSE 的差異，寫一份技術比較報告」

### 流程

```
使用者 ─── 「幫我研究 WebSocket 和 SSE...」 ──► Telegram Bot

Phase 1 — Planner:
  1. GeminiPlanner.Plan() 分析請求
  2. 判斷：需要「規劃 + 執行調研 + 產出報告」→ sequential
  3. 生成計畫：
     {
       "intent": "技術比較報告",
       "max_retries": 2,
       "plan": {
         "type": "sequential",
         "steps": [
           { "type": "step", "role": "planner",
             "instruction": "分析 WebSocket 與 SSE 差異，列出比較維度",
             "output_key": "plan" },
           { "type": "step", "role": "executor",
             "instruction": "根據 {plan?} 執行調研",
             "tools": ["shell_exec"],
             "output_key": "artifacts" },
           { "type": "step", "role": "reporter",
             "instruction": "根據 {plan?} 和 {artifacts?} 撰寫報告",
             "output_key": "summary" }
         ]
       }
     }

Phase 2 — Executor:
  4. Convert(PlanNode) → AgentNodeConfig:
     seq_0
       └─ planner_1  (LlmAgent)
       └─ executor_2 (LlmAgent, tools: [shell_exec])
       └─ reporter_3 (LlmAgent)

  5. Build ADK agent tree → runner.Run()

── Step: planner_1 ──
  6. planner role + instruction 載入
  7. 分析需求，產出結構化計畫
  8. 輸出存入 state["plan"]

── Step: executor_2 ──
  9. 讀取 {plan?}
  10. 逐步執行調研（可使用 shell_exec tool）
  11. 輸出存入 state["artifacts"]

── Step: reporter_3 ──
  12. 讀取 {plan?} 和 {artifacts?}
  13. 整理成報告格式
  14. 輸出存入 state["summary"]

  15. Agent tree 執行完畢，回傳 results = session state

Phase 3 — Evaluator:
  16. GeminiEvaluator.Evaluate(userPrompt, results)
  17. 判斷：報告涵蓋足夠維度，結論清晰 → satisfied = true

Phase 4 — Responder:
  18. GeminiResponder.Respond(userPrompt, results)
  19. 格式化 state["summary"] 為最終回覆

Telegram Bot:
  20. 發送報告給使用者
```

### session state 時間軸

```
執行後:
  state["plan"]      = "## 比較維度\n1. 通訊方向..."
  state["artifacts"] = "## 調研結果\n### WebSocket\n..."
  state["summary"]   = "## WebSocket vs SSE 技術比較\n..."
```

### 時序圖

```
使用者   Telegram   Orchestrator  planner_1  executor_2  reporter_3
  │         │            │            │           │           │
  │──msg──►│            │            │           │           │
  │         │──Run()────►│            │           │           │
  │         │            │─Phase1────►│ (plan)    │           │
  │         │            │            │           │           │
  │         │            │─Phase2─────────────────────────────►│
  │         │            │            │─write─────►           │
  │         │            │            │           │─write─────►│
  │         │            │            │           │           │─write
  │         │            │◄─results──────────────────────────│
  │         │            │─Phase3────►(eval)       │           │
  │         │            │─Phase4────►(respond)    │           │
  │         │◄─reply─────│            │           │           │
  │◄─reply──│            │            │           │           │
```

---

## 範例三：迭代任務（Loop 執行）

> 使用者：「幫我寫一段 Go 的 binary search 函數，要確保邊界條件正確」

### 流程

```
Phase 1 — Planner:
  1. 判斷：需要「寫程式碼 + 測試檢查 + 可能修改」→ loop
  2. 生成計畫：
     {
       "intent": "撰寫並驗證 binary search 實作",
       "max_retries": 1,
       "plan": {
         "type": "loop",
         "max_iterations": 5,
         "exit_condition": {
           "output_key": "evaluation",
           "pattern": "PASS"
         },
         "steps": [
           { "type": "step", "role": "worker",
             "instruction": "根據 {evaluation?} 修改 binary search 程式碼",
             "output_key": "draft" },
           { "type": "step", "role": "evaluator",
             "instruction": "檢查邊界條件，若全部正確回覆 PASS，否則列出問題",
             "output_key": "evaluation" }
         ]
       }
     }

Phase 2 — Executor:
  3. Convert(PlanNode) → AgentNodeConfig:
     loop_0 (max_iterations: 5)
       └─ seq_3 (body wrapper)
            └─ worker_1    (LlmAgent)
            └─ evaluator_2 (LlmAgent)
            └─ exit_checker_4 (sentinel，監控 evaluation key 是否含 "PASS")

── 第 1 輪 ──

  worker_1:
    4. {evaluation?} 為空（首次），根據需求撰寫第一版
    5. 輸出存入 state["draft"]

  evaluator_2:
    6. 讀取 {draft?}，檢查邊界條件
    7. 單元素陣列未正確處理 → 回覆問題清單
    8. 輸出存入 state["evaluation"] = "問題：未處理單元素..."

  exit_checker_4:
    9. 讀取 state["evaluation"]，不含 "PASS"
    10. 不 escalate → 繼續下一輪

── 第 2 輪 ──

  worker_1:
    11. 讀取 {evaluation?} 回饋，修正邊界條件
    12. 輸出覆寫 state["draft"]

  evaluator_2:
    13. 重新檢查：所有邊界條件通過
    14. 輸出 state["evaluation"] = "PASS — 所有邊界條件正確"

  exit_checker_4:
    15. 讀取 state["evaluation"]，含 "PASS"
    16. 發出 Actions.Escalate = true → 迴圈結束

Phase 3 — Evaluator:
  17. results 中含最終 state["draft"] 和 state["evaluation"]
  18. satisfied = true

Phase 4 — Responder:
  19. 格式化最終 draft 為回覆
```

### session state 時間軸

```
第 1 輪結束:
  state["draft"]      = "func BinarySearch(...) { ... (v1) }"
  state["evaluation"] = "問題：\n1. 未處理單元素陣列..."

第 2 輪結束:
  state["draft"]      = "func BinarySearch(...) { ... (v2，已修正) }"
  state["evaluation"] = "PASS — 所有邊界條件正確"
```

### 時序圖

```
使用者   Orchestrator  worker_1  evaluator_2  exit_checker_4
  │           │            │           │            │
  │──msg─────►│            │           │            │
  │           │─Phase1────►│(plan)     │            │
  │           │            │           │            │
  │           │─Phase2──────────────────────────────►│
  │           │            │           │            │
  │           │            │  Round 1  │            │
  │           │            │──draft───►│            │
  │           │            │           │──eval─────►│
  │           │            │           │  (不含PASS) │──不 escalate
  │           │            │           │            │
  │           │            │  Round 2  │            │
  │           │            │──draft───►│            │
  │           │            │  (修正版) │──eval─────►│
  │           │            │           │  (含PASS)   │──escalate!
  │           │◄─results────────────────────────────│
  │           │─Phase3────►(eval)      │            │
  │           │─Phase4────►(respond)   │            │
  │◄─reply────│            │           │            │
```

---

## 範例四：混合任務（嵌套計畫）

> 使用者：「幫我重構這個模組，把所有 callback 改成 channel pattern，並確保測試通過」

### 流程

```
Phase 1 — Planner:
  1. 判斷：需要「先規劃步驟 → 迭代修改直到測試通過 → 產出報告」
  2. 生成嵌套計畫：
     {
       "intent": "重構 callback 為 channel pattern",
       "max_retries": 1,
       "plan": {
         "type": "sequential",
         "steps": [
           {
             "type": "step", "role": "planner",
             "instruction": "分析重構範圍，制定步驟與驗收標準",
             "output_key": "plan"
           },
           {
             "type": "loop",
             "max_iterations": 5,
             "exit_condition": { "output_key": "evaluation", "pattern": "ALL_PASS" },
             "steps": [
               { "type": "step", "role": "worker",
                 "instruction": "根據 {plan?} 執行重構，參考 {evaluation?} 修正",
                 "tools": ["shell_exec"],
                 "output_key": "draft" },
               { "type": "step", "role": "evaluator",
                 "instruction": "執行 go test ./...，全部通過回覆 ALL_PASS",
                 "tools": ["shell_exec"],
                 "output_key": "evaluation" }
             ]
           },
           {
             "type": "step", "role": "reporter",
             "instruction": "根據 {plan?} 和最終 {draft?} 撰寫重構報告",
             "output_key": "summary"
           }
         ]
       }
     }

Phase 2 — Executor:
  3. Convert(PlanNode) → AgentNodeConfig:
     seq_0
       └─ planner_1   (LlmAgent)
       └─ loop_2      (max_iterations: 5)
       │    └─ seq_5  (body wrapper)
       │         └─ worker_3    (LlmAgent, tools: [shell_exec])
       │         └─ evaluator_4 (LlmAgent, tools: [shell_exec])
       │         └─ exit_checker_6 (sentinel, evaluation, ALL_PASS)
       └─ reporter_7  (LlmAgent)

── Phase: planner_1 ──
  4. 分析重構範圍，產出步驟
  5. state["plan"] = "## 重構計畫\n1. 找出所有 callback..."

── Phase: loop_2 迭代 ──
  (多輪 worker + evaluator，直到 exit_checker 偵測到 ALL_PASS)
  6. 第 N 輪：worker 重構 + evaluator 跑測試
  7. 測試全部通過後 exit_checker escalate

── Phase: reporter_7 ──
  8. 讀取 {plan?} 和 {draft?}
  9. 輸出 state["summary"] = "## 重構報告..."

Phase 3 — Evaluator:
  10. 確認報告完整，satisfied = true

Phase 4 — Responder:
  11. 格式化 state["summary"] 為最終回覆
```

### session state 完整時間軸

```
planner_1 執行後:
  state["plan"]       = "## 重構計畫\n1. 找出所有 callback..."

loop 第 1 輪後:
  state["draft"]      = "修改了 3 個函數，但 test_x 失敗..."
  state["evaluation"] = "有 2 個測試失敗: test_x, test_y"

loop 最後一輪後 (exit_checker 觸發):
  state["draft"]      = "所有函數重構完成"
  state["evaluation"] = "ALL_PASS — go test ./... 12/12 通過"

reporter_7 執行後:
  state["summary"]    = "## 重構報告\n成功將 5 個函數..."
```

---

## 完整元件互動圖

四個範例統一呈現的流程對比：

```
                      ┌───────────────────┐
                      │     使用者請求      │
                      └────────┬──────────┘
                               │
                      ┌────────▼──────────┐
                      │   cmd/telegram     │
                      │   (Driving Adapter)│
                      └────────┬──────────┘
                               │
                      ┌────────▼──────────┐
                      │   Orchestrator     │
                      │   4 Phase Loop     │
                      └──┬────┬────┬───┬──┘
                         │    │    │   │
         ┌───────────────┘    │    │   └──────────────────┐
         │                    │    │                      │
         ▼                    ▼    ▼                      ▼
 ┌───────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
 │ direct plan   │  │ sequential   │  │ loop plan    │  │ nested plan  │
 │ (範例一)       │  │ plan (範例二) │  │ (範例三)     │  │ (範例四)      │
 │               │  │              │  │              │  │              │
 │ Phase 1 短路  │  │ seq_0        │  │ loop_0       │  │ seq_0        │
 │ 立即回傳      │  │  planner_1   │  │  seq_3       │  │  planner_1   │
 └───────────────┘  │  executor_2  │  │   worker_1   │  │  loop_2      │
                    │  reporter_3  │  │   evaluator_2│  │   worker_3   │
                    └──────────────┘  │   exit_chk_4 │  │   evaluator_4│
                                      └──────────────┘  │   exit_chk_6 │
                                                        │  reporter_7  │
                                                        └──────────────┘

 簡單問答          多步驟調研         迭代改善           規劃+迭代+報告
```

### 資料儲存

```
prompts/                       ← Orchestrator 核心 prompt
  ├── plan.prompt               ← Planner 指令 (決定 direct/sequential/loop/parallel)
  ├── evaluate.prompt           ← Evaluator 指令 (判斷 satisfied)
  └── respond.prompt            ← Responder 指令 (格式化最終回覆)

agents/
  ├── planner/agent.prompt      ← planner role 的 prompt 模板
  ├── executor/agent.prompt     ← executor role 的 prompt 模板
  ├── reporter/agent.prompt     ← reporter role 的 prompt 模板
  ├── worker/agent.prompt       ← worker role 的 prompt 模板
  └── evaluator/agent.prompt    ← evaluator role 的 prompt 模板

data/sessions/                 ← Session state 持久化
  └── tg-<chatID>.jsonl        ← 每個 Telegram chat 一個 session
```
