# Agent Tree 設定檔指南

> 本文件說明 `agenttree.yaml` 的 schema 設計、各欄位含義，以及實際使用範例。

---

## 目錄

1. [設計哲學](#設計哲學)
2. [Schema 概覽](#schema-概覽)
3. [欄位說明](#欄位說明)
4. [Agent 類型對照](#agent-類型對照)
5. [範例設定](#範例設定)
6. [驗證規則](#驗證規則)
7. [進階用法](#進階用法)

---

## 設計哲學

Agent tree 的設定檔設計遵循以下原則：

1. **宣告式而非命令式**：描述「系統長什麼樣」，而非「怎麼建構」
2. **遞迴結構**：每個 node 都有相同的 schema，支援任意深度巢套
3. **慣例優於設定**：大部分欄位有合理預設值，最小設定只需 `name` 和 `type`
4. **資料和邏輯分離**：設定檔描述結構，prompt 描述行為，程式碼負責組裝

---

## Schema 概覽

```yaml
version: "1"              # Schema 版本號

defaults:                  # 全域預設值
  model: "gemini-3-flash"  # 預設 LLM model

root:                      # Root agent (遞迴的 AgentNodeConfig)
  name: root
  type: llm
  # ... 欄位同 sub_agents 中的每個 node
  sub_agents:
    - name: child1
      type: sequential
      sub_agents:
        - name: grandchild1
          type: llm
          # ... 任意深度
```

---

## 欄位說明

### 頂層欄位

| 欄位 | 類型 | 必填 | 說明 |
|------|------|------|------|
| `version` | string | 是 | Schema 版本，目前為 `"1"` |
| `defaults` | object | 否 | 全域預設值 |
| `defaults.model` | string | 否 | 預設 LLM model ID |
| `root` | AgentNodeConfig | 是 | Root agent 的定義 |

### AgentNodeConfig 欄位

每個 agent node（包括 root 和所有 sub_agents）都遵循相同的 schema：

| 欄位 | 類型 | 必填 | 適用類型 | 說明 |
|------|------|------|---------|------|
| `name` | string | 是 | 全部 | Agent 的唯一名稱，整棵樹中不可重複 |
| `type` | string | 是 | 全部 | Agent 類型：`llm` / `sequential` / `loop` / `parallel` |
| `description` | string | 否 | 全部 | 一行描述，LLM 根據此決定是否 transfer |
| `model` | string | 否 | llm | 覆寫 defaults.model |
| `prompt_file` | string | 否 | llm | Prompt 檔案路徑（相對於 agents/ 目錄） |
| `instruction` | string | 否 | llm | 內嵌的 system instruction（prompt_file 優先） |
| `output_key` | string | 否 | llm | 輸出存入 session state 的 key 名稱 |
| `max_iterations` | uint | 否 | loop | 最大迭代次數，0 表示無限制 |
| `tools` | []string | 否 | llm | 掛載的 built-in tool 名稱列表 |
| `mcp_servers` | []string | 否 | llm | 掛載的 MCP server 名稱列表 |
| `sub_agents` | []AgentNodeConfig | 條件 | 全部 | 子 agent 列表 |

### 必填規則

- `name` 和 `type` 永遠必填
- `sequential` 和 `parallel` 必須有 `sub_agents`
- `loop` 必須有 `sub_agents`
- `llm` 的 `sub_agents` 是選填的（用於 transfer 路由）

---

## Agent 類型對照

### `llm` — LLM Agent

```yaml
name: planner
type: llm
description: "Analyzes tasks and creates structured plans."
model: "gemini-3-flash-preview"      # 可省略，用 defaults.model
prompt_file: planner                  # 載入 agents/planner/agent.prompt
output_key: plan                      # 輸出寫入 state["plan"]
tools:                                # 掛載的 tools
  - shell_exec
mcp_servers:                          # 掛載的 MCP servers
  - example-mcp-server
```

**對應 ADK 建構：**
```go
llmagent.New(llmagent.Config{
    Name:        "planner",
    Description: "Analyzes tasks and creates structured plans.",
    Model:       geminiModel,
    Instruction: loadedPromptText,
    OutputKey:   "plan",
    Tools:       []tool.Tool{shellTool},
    Toolsets:    []tool.Toolset{mcpToolset},
    SubAgents:   childAgents,
})
```

### `sequential` — Sequential Agent

```yaml
name: plan_execute_report
type: sequential
description: "Runs planner, executor, and reporter in sequence."
sub_agents:
  - name: planner
    type: llm
    # ...
  - name: executor
    type: llm
    # ...
  - name: reporter
    type: llm
    # ...
```

**行為**：按 `sub_agents` 列表順序，依序執行每個子 agent。

### `loop` — Loop Agent

```yaml
name: refinement_loop
type: loop
max_iterations: 5
sub_agents:
  - name: worker
    type: llm
    output_key: draft
  - name: evaluator
    type: llm
    output_key: evaluation
```

**行為**：重複執行 `sub_agents`（按順序），直到：
1. 某個 sub-agent escalate（`event.Actions.Escalate = true`）
2. 達到 `max_iterations` 上限
3. `max_iterations: 0` 時無上限，只靠 escalate 結束

### `parallel` — Parallel Agent

```yaml
name: multi_perspective
type: parallel
sub_agents:
  - name: approach_a
    type: llm
    # ...
  - name: approach_b
    type: llm
    # ...
```

**行為**：同時執行所有 `sub_agents`，各自在獨立的 branch 中運行。

---

## 範例設定

### 最小設定（單一 Root Agent）

```yaml
version: "1"
defaults:
  model: "gemini-3-flash-preview"
root:
  name: root
  type: llm
  description: "Simple assistant"
  prompt_file: root
```

### 標準 Plan-Execute-Report

```yaml
version: "1"
defaults:
  model: "gemini-3-flash-preview"
root:
  name: root
  type: llm
  prompt_file: root
  sub_agents:
    - name: plan_execute_report
      type: sequential
      description: "For complex multi-step tasks."
      sub_agents:
        - name: planner
          type: llm
          prompt_file: planner
          output_key: plan
        - name: executor
          type: llm
          prompt_file: executor
          output_key: artifacts
          tools:
            - shell_exec
        - name: reporter
          type: llm
          prompt_file: reporter
          output_key: summary
```

### 完整生產設定

請參考專案根目錄的 `agenttree.yaml`，包含所有三種 workflow pattern 的完整定義。

---

## 驗證規則

設定檔在載入時會進行以下驗證：

1. `version` 不可為空
2. `root.name` 不可為空
3. 所有 agent 的 `name` 在整棵樹中必須唯一
4. `type` 必須是 `llm` / `sequential` / `loop` / `parallel` 之一
5. `sequential` 和 `parallel` 必須有至少一個 `sub_agents`
6. `loop` 必須有至少一個 `sub_agents`

驗證失敗時會回傳 `domain.ValidationError`，包含欄位路徑和原因。

---

## 進階用法

### Prompt 解析規則

`prompt_file` 的解析順序：
1. 如果設定了 `prompt_file: planner`，載入 `agents/planner/agent.prompt`
2. 如果沒設定 `prompt_file`，嘗試載入 `agents/<name>/agent.prompt`
3. 如果以上都不存在，使用 `instruction` 欄位的內嵌文字
4. 如果三者都沒有，建構失敗

### Model 繼承

```yaml
defaults:
  model: "gemini-3-flash-preview"    # 全域預設

root:
  name: root
  type: llm
  model: "gemini-3-pro-preview"      # 覆寫 — Root 用 Pro
  sub_agents:
    - name: planner
      type: llm                       # 繼承 defaults → 用 Flash
    - name: executor
      type: llm
      model: "gemini-3-pro-preview"   # 覆寫 — Executor 也用 Pro
```

### Tool 掛載

每個 `llm` agent 可以獨立掛載不同的 tools 和 MCP servers。
只有需要使用特定工具的 agent 才掛載，避免不必要的 tool 暴露：

```yaml
root:
  name: root
  type: llm
  tools: []                          # Root 不掛載任何 tool
  sub_agents:
    - name: executor
      type: llm
      tools:
        - shell_exec                  # 只有 Executor 可以執行 shell
      mcp_servers:
        - code-analyzer               # 只有 Executor 可以用程式碼分析器
```

### 巢套組合

Workflow agent types 可以自由巢套：

```yaml
# Sequential 中嵌入 Loop
- name: plan_iterate_report
  type: sequential
  sub_agents:
    - name: planner
      type: llm
    - name: loop
      type: loop
      max_iterations: 5
      sub_agents:
        - name: step        # Loop 中嵌入 Sequential
          type: sequential
          sub_agents:
            - name: worker
              type: llm
            - name: evaluator
              type: llm
    - name: reporter
      type: llm
```

```yaml
# Parallel 中的各分支各自有 Sequential 流程
- name: multi_approach
  type: parallel
  sub_agents:
    - name: approach_a
      type: sequential
      sub_agents:
        - name: a_worker
          type: llm
        - name: a_checker
          type: llm
    - name: approach_b
      type: sequential
      sub_agents:
        - name: b_worker
          type: llm
        - name: b_checker
          type: llm
```
