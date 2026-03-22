# MVP Design Spec：Context Window Memory Manager

> **Tech Stack:** Go + Google ADK Go v0.6
> **Version:** v1.0-MVP
> **Date:** 2026-03-22

---

## 決策紀錄

| 決策 | 結果 | 理由 |
|------|------|------|
| 技術棧 | Go + Google ADK Go v0.6 | 使用者選擇 |
| 整合方式 | ADK Plugin（純 Plugin） | MVP 最簡單；Plugin callbacks 涵蓋所有需求 |
| MVP Provider | Gemini only | ADK 原生支援，避免寫 adapter |
| Compress Worker 模型 | 支援獨立模型配置 | 用較便宜模型（如 Flash-Lite）降低壓縮成本 |
| Quality Guard | MVP 跳過 LLM 驗證，只記 metrics | 降低成本和複雜度 |
| 專案結構 | internal-first, 單一 `internal/memory/` package | MVP 精簡，等痛了再拆 |
| 壓縮觸發點 | 單一 checkpoint: `BeforeModelCallback` | 簡單明確 |
| Token 計數 | 兩層估算：離線 `genai/tokenizer` → 線上 `countTokens` API | bloom filter 思路，避免每輪都 call API |
| 壓縮與 Session 互動 | BeforeModel 中改寫 `req.Contents`，Session 歷史不動 | 壓縮是 view transformation，不是 data mutation |

---

## 範圍定義

本 MVP 只處理**一個 process（單一對話串）的 context window 管理**。
不涉及多 agent、IPC、scheduling、持久化。

### OS 術語對照

| 本文件用語 | OS 原始概念 | Agent 世界意義 |
|---|---|---|
| Memory | Physical memory | Context window（token 容量）|
| Allocation | malloc | 每輪對話新增的 token |
| Compress | Memory compaction / page compression | 將舊對話壓縮以回收 context 空間（lossy） |
| CompressStrategy | Pluggable compaction algorithm | 可抽換的壓縮演算法 |
| OOM | Out-of-memory | Context window 溢出 |
| PINNED | Kernel reserved memory | System prompt 等不可回收區段 |
| BUFFER | Free memory reserve | 預留給下一輪回應生成的空間 |

---

## 架構

### 元件

| 元件 | 職責 |
|------|------|
| `MemoryPlugin` | ADK Plugin。唯一入口：BeforeModel 觸發壓縮判斷 + request 改寫；AfterModel 存 usage 數字 |
| `CompressStrategy` | Go interface + Generational 實作 |
| `ModelProfile` | Gemini model specs（context window, max output, cost） |
| `MemoryLayout` | Segment 配比（PINNED/SUMMARY/ACTIVE/BUFFER）+ BUFFER 自動調高 |
| Compress Agent | 獨立 ADK Runner + LLMAgent，用較便宜模型執行壓縮 |

### 核心資料流（每輪對話）

```
User msg → Runner → 組裝 LLMRequest
  → MemoryPlugin.BeforeModelCallback
      │
      ├─ 離線估算：
      │   last_total_tokens（上輪 response 的 UsageMetadata）
      │   + tokenizer.CountTokens(新 user msg)
      │   + max_output_tokens
      │   = estimated_total
      │
      ├─ estimated_total < 80% context window?
      │   → YES: pass through
      │   → NO: countTokens API 精算
      │         → < 80%? pass through（false alarm）
      │         → >= 80%? 觸發壓縮 ↓
      │
      ├─ 壓縮：
      │   Compress Agent（獨立 Runner）壓縮舊 turns
      │   改寫 req.Contents = [system prompt + summary + recent turns + user msg]
      │
      └─ pass through
  → Gemini API call → response
  → MemoryPlugin.AfterModelCallback
      └─ last_total_tokens = resp.UsageMetadata.TotalTokenCount
  → 完成
```

### 專案結構

```
agenticsystem/
├── go.mod
├── cmd/agent/
│   └── main.go                 # Demo: ADK Runner + MemoryPlugin + demo agent
├── internal/memory/
│   ├── profile.go              # S0: ModelProfile struct + Gemini defaults
│   ├── layout.go               # S1: Segments + config + BUFFER 自動調高
│   ├── plugin.go               # MemoryPlugin: BeforeModel + AfterModel
│   ├── compress.go             # S4: CompressStrategy interface + Generational
│   ├── metrics.go              # Observability: 簡單 struct 記錄
│   └── memory_test.go
└── configs/
    └── default.json            # 預設配置
```

---

## S0：Model Profile

### 結構

```go
type ModelProfile struct {
    ModelID             string
    Provider            string  // "google"
    ContextWindowTokens int
    MaxOutputTokens     int

    // Cost
    CostPer1KInputTokens  float64
    CostPer1KOutputTokens float64

    // Compress Worker（可選）
    CompressModelID             string
    CompressCostPer1KInputTokens  float64
    CompressCostPer1KOutputTokens float64
}
```

### MVP 內建 Profiles

| Model | Context Window | Max Output |
|---|---|---|
| `gemini-2.0-flash` | 1,048,576 | 8,192 |
| `gemini-2.0-flash-lite` | 1,048,576 | 8,192 |
| `gemini-2.5-pro` | 1,048,576 | 65,536 |
| `gemini-2.5-flash` | 1,048,576 | 65,536 |

### Acceptance Criteria

- 指定已知 model_id → 自動載入對應 profile
- 提供自訂 profile → 覆蓋預設值；缺必填欄位（model_id, context_window_tokens）→ error
- 未指定 compress_model_id → fallback 用主模型 + warning log

---

## S1：Memory Layout

### Segment 定義

| Segment | 用途 | 預設配比 |
|---|---|---|
| `PINNED` | System prompt + tool schema | 15% |
| `SUMMARY` | 壓縮摘要 | 25% |
| `ACTIVE` | 近期完整對話 | 50% |
| `BUFFER` | 預留回應空間 | 10% |

### 規則

- `BUFFER >= max_output_tokens`（取較大值）
- 配比總和 != 100% → error
- BUFFER 自動調高時，差額按比例從其他 segment 扣除

### Acceptance Criteria

- 各 segment token 上限正確計算
- 小 context window 模型 BUFFER 被自動調高至 max_output_tokens
- 配比總和 != 100% → ConfigurationError

---

## S2+S3：Token Tracking + Compress Trigger（合併於 MemoryPlugin）

### 機制

**Token 來源：**

| 計數對象 | 來源 | 時機 |
|---|---|---|
| 累積用量 | 上輪 `resp.UsageMetadata.TotalTokenCount` | AfterModelCallback 存一個數字 |
| 新 user msg 估算 | `genai/tokenizer.CountTokens()` 離線 | BeforeModelCallback |
| 精確計數 | `countTokens` API | 離線估算超閾值時才呼叫 |

**觸發條件：**

```
estimated_total = last_total_tokens
                + tokenizer.CountTokens(new_user_msg)
                + max_output_tokens

if estimated_total >= 80% * context_window_tokens:
    precise_total = countTokens_API(full_request) + max_output_tokens
    if precise_total >= 80% * context_window_tokens:
        trigger_compress()
```

- 閾值 80% 可配置
- 離線估算偏保守（寧可多觸發 countTokens，不可漏判）

### Acceptance Criteria

- 短對話（遠低於閾值）→ 從未呼叫 countTokens API
- 接近閾值時 → 離線估算觸發 countTokens API 精算
- 精算仍超閾值 → 壓縮被觸發
- 精算未超（false alarm）→ pass through
- AfterModelCallback 只做一件事：存 `last_total_tokens`

---

## S4：Compress Execution

### CompressStrategy Interface

```go
type CompressStrategy interface {
    Name() string

    // 從 turns 中選出要壓縮的候選
    SelectCandidates(
        activeTurns []ConversationTurn,
        targetReclaimTokens int,
    ) []ConversationTurn

    // 執行壓縮，使用獨立 Compress Agent
    Compress(
        candidates []ConversationTurn,
        existingSummary string,
        profile ModelProfile,
    ) (*CompressResult, error)
}

type CompressResult struct {
    CompressedText       string
    OriginalTokens       int
    CompressedTokens     int
    ActualCompressionRatio float64
    Cost                 float64
    WorkerUsage          UsageMetadata
}
```

### MVP 策略：Generational

- 選最舊的 N 輪（預設 5）壓縮成摘要
- 使用獨立 ADK Runner + Compress Agent（隔離 context window）
- 優先使用 `CompressModelID`（較便宜模型）

### 壓縮後 request 改寫

```
BeforeModelCallback 中改寫 req.Contents:
  [system_prompt] + [summary（壓縮結果）] + [recent_active_turns] + [user_msg]
```

- Session 原始歷史不動（view transformation, not data mutation）
- Plugin 內部維護：`summaries []string` + `compressedUpToIndex int`

### Acceptance Criteria

- 壓縮使用獨立 Runner（不影響主 context window）
- 壓縮前後 token 計數變化被記錄
- 策略可在初始化時切換（透過 config）
- 自訂 strategy 缺必要方法 → error

---

## S6：OOM Handler（簡化版）

### 機制

當壓縮後 `precise_total` 仍 >= 90%（emergency threshold）：

1. 對 SUMMARY 再次壓縮（摘要的摘要）
2. 仍不夠 → 回傳 `OOMWarning` event，建議開新對話
3. **絕對不靜默截斷**

### Acceptance Criteria

- 極小 context window 跑長對話 → OOM handler 最終被觸發
- 任何情況下都不靜默丟棄內容

---

## Observability

MVP 需記錄的 metrics：

| Metric | 類型 | 用途 |
|---|---|---|
| `last_total_tokens` | Gauge | 每輪的累積用量 |
| `usage_ratio` | Gauge | last_total_tokens / context_window |
| `compress_trigger_count` | Counter | 壓縮觸發次數 |
| `compress_reclaimed_tokens` | Histogram | 每次壓縮回收量 |
| `compression_ratio` | Histogram | 壓縮率分佈 |
| `countTokens_api_call_count` | Counter | countTokens API 呼叫次數（越少越好） |
| `compress_cost` | Counter($) | 壓縮累計成本 |
| `oom_event_count` | Counter | OOM 次數（目標：0）|

---

## 不在 MVP 範圍

| 功能 | 為何延後 |
|---|---|
| 非 Gemini provider | ADK 原生只支援 Gemini |
| Quality Guard（LLM 驗證） | 成本高，先驗證壓縮流程本身 |
| 跨 session 持久化 | 先穩定單 session |
| 多 agent 協作 | 需先有穩定 single-process |
| Runtime strategy 熱切換 | MVP 只支援初始化時選擇 |
| ManagedSessionService | 單一 Plugin 足以處理 MVP 需求 |
| PINNED 動態擴展 | MVP 先假設 system prompt 固定 |

---

## 實作順序

```
S0 (ModelProfile) → S1 (MemoryLayout) → S2+S3 (Plugin: tracking + trigger)
  → S4 (CompressStrategy + Generational) → S6 (OOM Handler) → Demo Agent
```
