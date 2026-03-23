# 端到端流程範例

> 本文件以四個具體範例說明使用者訊息從 Telegram 進入系統、經過 Root 判斷、到 workflow 執行、到最終回覆的完整流程。

---

## 目錄

1. [系統流程概覽](#系統流程概覽)
2. [範例一：簡單對話](#範例一簡單對話)
3. [範例二：複雜任務 (Plan-Execute-Report)](#範例二複雜任務-plan-execute-report)
4. [範例三：迭代任務 (Iterative Refinement)](#範例三迭代任務-iterative-refinement)
5. [範例四：混合任務 (Plan-Iterate-Report)](#範例四混合任務-plan-iterate-report)
6. [完整元件互動圖](#完整元件互動圖)

---

## 系統流程概覽

所有請求都遵循相同的入口流程：

```
使用者 (Telegram)
    │ 發送訊息
    ▼
cmd/telegram/main.go
    │ 建立 session, 呼叫 runner.Run()
    ▼
ADK Runner
    │ 驅動 agent tree
    ▼
Root Agent
    │ 意圖判斷
    ├─ 簡單 → 直接回覆
    └─ 複雜 → transfer 給 workflow
              │
              ▼
         Workflow 執行
              │
              ▼
         結果寫入 session state
              │
              ▼
         Root 整理回覆 (或 workflow 直接回覆)
              │
              ▼
cmd/telegram/main.go
    │ 收集 event 中的 reply text
    ▼
使用者 (Telegram) 收到回覆
```

---

## 範例一：簡單對話

> 使用者：「今天天氣如何？」

### 流程

```
使用者 ─── 「今天天氣如何？」 ──► Telegram Bot

Telegram Bot:
  1. 建立/取得 session (tg-<chatID>)
  2. 呼叫 runner.Run(userMessage)

ADK Runner:
  3. 調用 Root Agent

Root Agent:
  4. 意圖判斷：這是一個簡單問答，不需要 workflow
  5. 直接使用自己的 LLM 能力回覆
  6. 產出 event: "我沒有即時天氣資料，建議您查看中央氣象署網站..."

ADK Runner:
  7. 收集 Root Agent 的 event
  8. 回傳給 Telegram Bot

Telegram Bot:
  9. 將回覆文字發送給使用者
```

### 為什麼不轉交？

Root Agent 的 prompt 中定義了路由規則。簡單問答、閒聊、單步查詢都歸 Root 直接處理。
不需要規劃也不需要迭代的任務，轉交 workflow 只是浪費 token。

### Session State 變化

```
(無 state 變化 — Root 直接回覆不寫入 state)
```

---

## 範例二：複雜任務 (Plan-Execute-Report)

> 使用者：「幫我研究 WebSocket 和 SSE 的差異，寫一份技術比較報告」

### 流程

```
使用者 ─── 「幫我研究 WebSocket 和 SSE...」 ──► Telegram Bot

Telegram Bot:
  1. 建立/取得 session
  2. 呼叫 runner.Run(userMessage)

ADK Runner:
  3. 調用 Root Agent

Root Agent:
  4. 意圖判斷：
     - 需要「規劃研究方向 + 執行調研 + 產出報告」
     - 匹配 Plan-Execute-Report pattern
  5. transfer 給 plan_execute_report

ADK Runner:
  6. 切換到 SequentialAgent: plan_execute_report
  7. 依序執行 sub-agents:

── Step 1: Planner ──
  8. Planner 讀取使用者需求
  9. 產出結構化計畫：
     ```
     ## 任務理解
     比較 WebSocket 和 SSE 兩種即時通訊技術

     ## 執行步驟
     1. 整理 WebSocket 的核心特性和使用場景
     2. 整理 SSE 的核心特性和使用場景
     3. 建立比較表格（通訊方向、協定、瀏覽器支援等）
     4. 給出選擇建議

     ## 驗收標準
     - 涵蓋至少 5 個比較維度
     - 包含具體的選擇建議
     ```
  10. 輸出存入 state["plan"]

── Step 2: Executor ──
  11. Executor 讀取 {plan}
  12. 逐步執行計畫（可使用 shell_exec tool 查資料）
  13. 產出研究結果和比較表格
  14. 輸出存入 state["artifacts"]

── Step 3: Reporter ──
  15. Reporter 讀取 {plan} 和 {artifacts}
  16. 整理成使用者友善的報告格式
  17. 輸出存入 state["summary"]
  18. SequentialAgent 結束，控制權回到 Root

Root Agent:
  19. 讀取 state["summary"]
  20. 整理成最終回覆

Telegram Bot:
  21. 發送報告給使用者
```

### Session State 變化

```
state["plan"]      = "## 任務理解\n比較 WebSocket..."
state["artifacts"] = "## 執行結果\n### WebSocket\n..."
state["summary"]   = "## WebSocket vs SSE 技術比較\n..."
```

### 時序圖

```
使用者    Telegram   Root     Planner   Executor   Reporter
  │         │        │         │          │          │
  │──msg──►│        │         │          │          │
  │         │──run──►│         │          │          │
  │         │        │         │          │          │
  │         │        │transfer │          │          │
  │         │        │────────►│          │          │
  │         │        │         │──plan───►│          │
  │         │        │         │  (state) │          │
  │         │        │         │          │──arts───►│
  │         │        │         │          │ (state)  │
  │         │        │         │          │          │──summary
  │         │        │         │          │          │  (state)
  │         │        │◄──────escalate─────────────────│
  │         │        │         │          │          │
  │         │◄─reply─│         │          │          │
  │◄─reply──│        │         │          │          │
```

---

## 範例三：迭代任務 (Iterative Refinement)

> 使用者：「幫我寫一段 Go 的 binary search 函數，要確保邊界條件正確」

### 流程

```
使用者 ─── 「幫我寫 binary search...」 ──► Telegram Bot

Root Agent:
  1. 意圖判斷：
     - 需要「寫程式碼 + 檢查正確性 + 可能需要修改」
     - 匹配 Iterative Refinement pattern
  2. transfer 給 iterative_refinement

LoopAgent: iterative_refinement (max_iterations: 5)

── 第 1 輪 ──

  Worker:
    3. 讀取使用者需求（首次迭代，{evaluation?} 為空）
    4. 產出第一版 binary search 程式碼
    5. 輸出存入 state["draft"]

  Evaluator:
    6. 讀取 {draft}
    7. 檢查程式碼：
       - 空陣列處理？ 有
       - 單元素陣列？ 沒處理
       - 重複元素？ 沒處理
    8. 評估結果：需要修改
       ```
       ## 評估結果：需要修改

       ### 問題清單
       1. 未處理單元素陣列的邊界條件
       2. 未處理重複元素的情況（應回傳第一個出現的位置）

       ### 優先處理
       先修正單元素陣列的邊界條件
       ```
    9. 輸出存入 state["evaluation"]
    10. 不 escalate → 繼續迴圈

── 第 2 輪 ──

  Worker:
    11. 讀取 {evaluation} 的回饋
    12. 修改程式碼，處理單元素和重複元素
    13. 輸出覆寫 state["draft"]

  Evaluator:
    14. 讀取新的 {draft}
    15. 重新檢查：所有邊界條件都已處理
    16. 評估結果：通過
        ```
        ## 評估結果：通過

        ### 達成項目
        - 空陣列正確回傳 -1
        - 單元素陣列正確處理
        - 重複元素回傳第一個出現的位置
        - 整體邏輯清晰
        ```
    17. escalate → 結束迴圈

Root Agent:
  18. 讀取 state["draft"]（最終版本的程式碼）
  19. 整理成回覆

使用者收到最終版的 binary search 程式碼
```

### Session State 變化（時間軸）

```
第 1 輪結束:
  state["draft"]      = "func BinarySearch(arr []int, target int) int { ... (v1) }"
  state["evaluation"] = "## 評估結果：需要修改\n### 問題清單\n1. 未處理單元素..."

第 2 輪結束:
  state["draft"]      = "func BinarySearch(arr []int, target int) int { ... (v2，已修正) }"
  state["evaluation"] = "## 評估結果：通過\n### 達成項目\n..."
```

### 時序圖

```
使用者   Root    LoopAgent   Worker   Evaluator
  │       │         │          │          │
  │──msg─►│         │          │          │
  │       │transfer │          │          │
  │       │────────►│          │          │
  │       │         │          │          │
  │       │         │ Round 1  │          │
  │       │         │─────────►│          │
  │       │         │          │──draft──►│
  │       │         │          │          │──eval (需修改)
  │       │         │          │          │  (不 escalate)
  │       │         │          │          │
  │       │         │ Round 2  │          │
  │       │         │─────────►│          │
  │       │         │  (讀eval)│          │
  │       │         │          │──draft──►│
  │       │         │          │          │──eval (通過)
  │       │         │          │          │  (escalate!)
  │       │         │◄────────────────────│
  │       │◄────────│          │          │
  │◄reply─│         │          │          │
```

---

## 範例四：混合任務 (Plan-Iterate-Report)

> 使用者：「幫我重構這個模組，把所有 callback 改成 channel pattern，並確保測試通過」

### 流程

```
Root Agent:
  1. 意圖判斷：
     - 需要「先規劃重構步驟」（Plan）
     - 然後「迭代修改直到測試通過」（Iterate）
     - 最後「產出重構報告」（Report）
     - 匹配 Plan-Iterate-Report pattern
  2. transfer 給 plan_iterate_report

SequentialAgent: plan_iterate_report

── Phase 1: Plan ──

  Planner:
    3. 分析使用者需求
    4. 產出重構計畫：
       ```
       ## 執行步驟
       1. 找出所有使用 callback 的地方
       2. 設計 channel-based 替代方案
       3. 逐一重構，每次修改一個函數
       4. 確保每次修改後測試通過

       ## 驗收標準
       - 所有 callback 都改成 channel pattern
       - go test ./... 全部通過
       - 無 race condition (go test -race)
       ```
    5. 輸出存入 state["plan"]

── Phase 2: Iterate ──

  LoopAgent: iterate_execute (max_iterations: 5)

  ── 第 1 輪 ──

    Worker:
      6. 讀取 {plan}，開始重構
      7. 修改第一批函數
      8. 執行 go test ./... (使用 shell_exec tool)
      9. 輸出存入 state["draft"]

    Evaluator:
      10. 讀取 {draft} 和 {plan} 中的驗收標準
      11. 檢查：測試有 2 個失敗
      12. 評估：需要修改，列出失敗的測試
      13. 不 escalate

  ── 第 2 輪 ──

    Worker:
      14. 讀取 evaluation 回饋，修正問題
      15. 再次執行測試
      16. 輸出覆寫 state["draft"]

    Evaluator:
      17. 檢查：測試全部通過
      18. 跑 go test -race：無 race condition
      19. 評估：通過
      20. escalate 結束迴圈

── Phase 3: Report ──

  Reporter:
    21. 讀取 {plan}, {draft}, {evaluation}
    22. 產出重構報告：
        ```
        ## 重構報告

        ### 摘要
        成功將 5 個 callback-based 函數改為 channel pattern。
        所有 12 個測試通過，無 race condition。

        ### 修改清單
        1. processData() — callback -> channel
        2. handleEvent() — callback -> channel
        ...

        ### 測試結果
        - go test ./... : PASS (12/12)
        - go test -race : PASS
        ```
    23. 輸出存入 state["summary"]

Root Agent:
  24. 讀取 state["summary"]
  25. 整理回覆給使用者

使用者收到完整的重構報告
```

### Session State 完整時間軸

```
Phase 1 結束:
  state["plan"]       = "## 執行步驟\n1. 找出所有..."

Phase 2, Round 1:
  state["draft"]      = "修改了 3 個函數，但 test_x 和 test_y 失敗..."
  state["evaluation"] = "## 需要修改\n1. test_x 失敗因為..."

Phase 2, Round 2:
  state["draft"]      = "所有函數已修改，測試全部通過"
  state["evaluation"] = "## 通過\n所有測試通過，無 race condition"

Phase 3 結束:
  state["summary"]    = "## 重構報告\n### 摘要\n成功將 5 個..."
```

---

## 完整元件互動圖

以下是四個範例統一呈現在同一張圖中：

```
                          ┌───────────────────┐
                          │     使用者請求      │
                          └────────┬──────────┘
                                   │
                          ┌────────▼──────────┐
                          │   cmd/telegram     │
                          │   (Driving Adapter) │
                          └────────┬──────────┘
                                   │
                          ┌────────▼──────────┐
                          │   ADK Runner       │
                          │   + PluginConfig   │
                          │   + SessionService │
                          └────────┬──────────┘
                                   │
                          ┌────────▼──────────┐
                          │   Root Agent       │
                          │   (LlmAgent)       │
                          │                    │
                          │   意圖判斷 →        │
                          └──┬────┬────┬───┬──┘
                             │    │    │   │
             ┌───────────────┘    │    │   └──────────────────┐
             │                    │    │                      │
             ▼                    ▼    ▼                      ▼
     ┌───────────┐    ┌──────────┐    ┌──────────┐    ┌──────────────┐
     │ 直接回覆   │    │ PER      │    │ IR       │    │ PIR          │
     │ (範例一)   │    │ (範例二) │    │ (範例三) │    │ (範例四)      │
     └───────────┘    │          │    │          │    │              │
                      │Sequential│    │  Loop    │    │Sequential    │
                      │ Planner  │    │ Worker   │    │ Planner      │
                      │ Executor │    │ Evaluator│    │ Loop         │
                      │ Reporter │    │          │    │  Worker      │
                      └──────────┘    └──────────┘    │  Evaluator   │
                                                      │ Reporter     │
                                                      └──────────────┘

     簡單問答         多步驟任務       迭代改進任務      規劃+迭代+報告

PER = Plan-Execute-Report
IR  = Iterative Refinement
PIR = Plan-Iterate-Report
```

### 依賴關係圖

```
agenttree.yaml ────► infra/config/agenttree/loader.go
                           │
                           ▼
                     domain.AgentTreeConfig
                           │
                           ▼
                     application/agenttree/builder.go
                           │
                     ┌─────┼─────┬──────────┐
                     │     │     │          │
                     ▼     ▼     ▼          ▼
                   LLM   Seq   Loop      Parallel
                  Agent  Agent  Agent     Agent
                     │
              ┌──────┼──────┐
              │      │      │
              ▼      ▼      ▼
          agentdef  gemini  mcptoolset
          (prompt)  (model)  (tools)
```

### 資料儲存

```
agents/
  ├── root/agent.prompt        ← Root Agent 的 prompt
  ├── planner/agent.prompt     ← Planner 的 prompt
  ├── executor/agent.prompt    ← Executor 的 prompt
  ├── reporter/agent.prompt    ← Reporter 的 prompt
  ├── worker/agent.prompt      ← Worker 的 prompt
  └── evaluator/agent.prompt   ← Evaluator 的 prompt

agenttree.yaml                 ← 整棵 agent tree 的結構定義

data/sessions/                 ← Session state 持久化
  └── tg-<chatID>.jsonl       ← 每個 Telegram chat 一個 session
```
