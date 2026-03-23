# AgenticSystem Architecture

> Explicit Architecture (Hexagonal / Ports & Adapters / Clean Architecture / DDD)

## Dependency Rule

```
cmd/ (User Interface)  ──→  internal/core/  ←──  internal/infra/ (Infrastructure)
                             (Application Core)
```

- Dependencies always point **inward** toward the Core.
- `cmd/` (Driving Adapters) calls into Core.
- `infra/` (Driven Adapters) implements Core Ports.
- Core **never** depends on `cmd/` or `infra/`.
- `cmd/` **never** depends on `infra/` directly (except composition root wiring).

---

## Directory Structure

```
AgenticSystem/
│
├── cmd/                                        # USER INTERFACE (Driving Adapters)
│   │                                           # Go 慣例的 entrypoint 目錄。
│   │                                           # 每個子目錄是一個 delivery mechanism，
│   │                                           # 負責把外部輸入轉譯為 Core 的呼叫。
│   │
│   ├── agent/                                  # CLI Driving Adapter
│   │   └── main.go                             #   互動式終端 agent
│   ├── telegram/                               # Telegram Bot Driving Adapter
│   │   └── main.go                             #   Telegram webhook/polling 入口
│   └── web/                                    # Web UI Driving Adapter
│       └── main.go                             #   HTTP server 入口
│
├── internal/                                   # APPLICATION CORE + INFRASTRUCTURE
│   │
│   ├── core/                                   # APPLICATION CORE (不依賴外部)
│   │   │                                       # 這是整個系統的心臟。
│   │   │                                       # core/ 內的任何 package 都不可以 import
│   │   │                                       # internal/infra/ 或 cmd/。
│   │   │
│   │   ├── domain/                             # DOMAIN LAYER (最內層)
│   │   │   │                                   # 純業務概念。零外部依賴，只用 stdlib。
│   │   │   │                                   # 放 entities、value objects、domain services。
│   │   │   │
│   │   │   └── model.go                        # Domain Model
│   │   │                                       #   - AgentDefinition (entity)
│   │   │                                       #   - MCPServerConfig (value object)
│   │   │                                       #   - MCPConfig (value object)
│   │   │                                       #   - SessionState (value object, future)
│   │   │                                       #   - WorkflowSpec (value object, future)
│   │   │
│   │   ├── port/                               # PORTS (Core 對外部的需求合約)
│   │   │   │                                   # Port = interface，定義 Core 需要什麼能力。
│   │   │   │                                   # Port 的設計以 Core 的需求為準，
│   │   │   │                                   # 絕不鏡像外部工具的 API。
│   │   │   │                                   # Port 可以 import domain types 和 ADK types。
│   │   │   │
│   │   │   ├── agent.go                        # AgentLoader port
│   │   │   │                                   #   Load(baseDir, name) → *domain.AgentDefinition
│   │   │   │
│   │   │   ├── tool.go                         # ToolProvider port
│   │   │   │                                   #   Provide(cfg) → ([]tool.Tool, []tool.Toolset)
│   │   │   │
│   │   │   ├── session.go                      # SessionStore port
│   │   │   │                                   #   (ADK 的 session.Service 已經是 interface，
│   │   │   │                                   #    這裡可以 re-export 或加 domain-specific 擴展)
│   │   │   │
│   │   │   └── memory.go                       # MemoryService port (future)
│   │   │                                       #   壓縮、持久化、長期記憶
│   │   │
│   │   └── application/                        # APPLICATION LAYER
│   │       │                                   # Use case 編排。薄 orchestrator —
│   │       │                                   # 從 port 取得依賴，呼叫 domain logic，
│   │       │                                   # 組裝最終結果。業務邏輯不在這裡。
│   │       │
│   │       └── wire.go                         # Composition Root / Application Service
│   │                                           #   New(ctx, cfg) → *App
│   │                                           #   接收 port implementations 作為參數，
│   │                                           #   組裝 agent、tools、plugins、runner。
│   │                                           #   這是唯一允許同時碰 port 和 infra 的地方。
│   │
│   ├── infra/                                  # INFRASTRUCTURE (Driven Adapters)
│   │   │                                       # 實作 Core Ports。按 tool type 分組，
│   │   │                                       # 再按 vendor/implementation 分。
│   │   │                                       # 換供應商 = 在同一個 tool type 下加新 adapter。
│   │   │
│   │   ├── config/                             # Configuration Adapters (filesystem I/O)
│   │   │   ├── agentdef/                       #   Agent definition loader
│   │   │   │   ├── loader.go                   #     implements port.AgentLoader
│   │   │   │   └── override.go                 #     instruction override plugin
│   │   │   └── mcpconfig/                      #   MCP config loader
│   │   │       ├── loader.go                   #     reads mcp.json → domain.MCPConfig
│   │   │       ├── loader_test.go
│   │   │       └── integration_test.go
│   │   │
│   │   ├── llm/                                # LLM Provider Adapters
│   │   │   └── gemini/                         #   Google Gemini adapter (future extraction)
│   │   │                                       #     目前 Gemini client 建構在 wire.go 內，
│   │   │                                       #     Phase 1 時抽出為獨立 adapter。
│   │   │
│   │   ├── persistence/                        # Persistence Adapters
│   │   │   └── jsonl/                          #   JSONL-based session store
│   │   │       ├── service.go                  #     implements session.Service
│   │   │       └── service_test.go
│   │   │
│   │   ├── tooling/                            # Tool Execution Adapters
│   │   │   ├── shell/                          #   Shell command execution
│   │   │   │   ├── tool.go                     #     ShellInput/Output + Handler
│   │   │   │   └── tool_test.go
│   │   │   └── mcp/                            #   MCP toolset adapter (future)
│   │   │                                       #     implements port.ToolProvider
│   │   │                                       #     目前 MCP toolset 建構在 wire.go 內，
│   │   │                                       #     Phase 1 時抽出為獨立 adapter。
│   │   │
│   │   └── observe/                            # Observability Adapters
│   │       ├── debug/                          #   Debug request dump plugin
│   │       │   └── plugin.go
│   │       └── memory/                         #   Memory compression plugin
│   │           ├── compress.go
│   │           ├── compress_test.go
│   │           ├── plugin.go
│   │           ├── plugin_test.go
│   │           ├── profile.go
│   │           ├── profile_test.go
│   │           └── oom_test.go
│   │
│   └── shared/                                 # SHARED KERNEL (future)
│       │                                       # 跨 component 共享的 types。
│       │                                       # 目前只有一個 bounded context，
│       │                                       # Phase 1 multi-agent 時會需要：
│       │                                       #   - Agent 間通訊的 event types
│       │                                       #   - 共享的 state key 定義
│       │                                       #   - Entity IDs
│       │
│       └── event/                              # Cross-component events (future)
│
├── agents/                                     # AGENT DEFINITIONS (data, not code)
│   │                                           # 每個子目錄是一個 agent 的完整定義。
│   │                                           # 包含 prompt、MCP config、未來的 tool binding。
│   │
│   └── demo_agent/
│       ├── agent.prompt                        # System instruction (dotprompt format)
│       └── mcp.json                            # MCP server configuration
│
├── docs/                                       # Documentation
│   ├── adr/                                    # Architecture Decision Records
│   └── specs/                                  # Design specifications
│
├── configs/                                    # Runtime configuration
├── testdata/                                   # Test fixtures
│
├── go.mod
├── go.sum
├── .gitignore
└── ARCHITECTURE.md                             # ← 你正在讀的這個檔案
```

---

## Layer Responsibilities

### cmd/ — User Interface (Driving Adapters)

| 職責 | 說明 |
|------|------|
| **做什麼** | 把外部輸入（CLI stdin、Telegram message、HTTP request）轉譯為 Core 的呼叫 |
| **不做什麼** | 不含業務邏輯、不直接操作 infra |
| **依賴方向** | → `internal/core/application/` (呼叫 `wire.New()` 組裝 App) |
| **例外** | Composition root wiring 時可以 import `infra/` 的具體型別來注入 |

### internal/core/domain/ — Domain Layer

| 職責 | 說明 |
|------|------|
| **做什麼** | 定義純業務概念：entities、value objects、domain services、domain events |
| **不做什麼** | 不 import 任何 infra、不 import ADK/genai/MCP SDK |
| **依賴方向** | 零外部依賴，只用 Go stdlib |
| **設計原則** | 換掉所有外部框架（ADK、Gemini、Telegram），domain 層不需要改一行 |

### internal/core/port/ — Ports

| 職責 | 說明 |
|------|------|
| **做什麼** | 定義 Core 對外部世界的需求合約（interfaces） |
| **不做什麼** | 不含實作，不鏡像外部工具 API |
| **依賴方向** | 可 import `core/domain/` types、可 import ADK framework types |
| **設計原則** | Port 的設計以 Core 的需求為準：「我需要載入 agent 定義」而非「dotprompt 可以做什麼」 |

### internal/core/application/ — Application Layer

| 職責 | 說明 |
|------|------|
| **做什麼** | Use case 編排 — 接收 port implementations，組裝 domain objects，驅動業務流程 |
| **不做什麼** | 不含業務邏輯（那是 domain 的事），不直接 import infra |
| **依賴方向** | → `core/domain/`、→ `core/port/` |
| **目前** | `wire.go` 是 composition root，組裝 agent + tools + plugins + runner |

### internal/infra/ — Infrastructure (Driven Adapters)

| 職責 | 說明 |
|------|------|
| **做什麼** | 實作 Core Ports — 連接外部工具（filesystem、Gemini API、MCP server...） |
| **不做什麼** | 不含業務邏輯、不互相依賴（`config/` 不 import `tooling/`） |
| **依賴方向** | → `core/domain/` (for domain types)、→ `core/port/` (implements interfaces) |
| **分組規則** | 按 tool type 分，再按 vendor/implementation 分 |

#### infra/ 子目錄對照

| Tool Type | Adapter | 實作的 Port | 說明 |
|-----------|---------|-------------|------|
| `config/agentdef` | Filesystem + dotprompt | `port.AgentLoader` | 讀 agent.prompt |
| `config/mcpconfig` | Filesystem + JSON | (returns domain.MCPConfig) | 讀 mcp.json |
| `llm/gemini` | Google Gemini API | (future: `port.LLMProvider`) | LLM 呼叫 |
| `persistence/jsonl` | JSONL files | `session.Service` (ADK) | Session 持久化 |
| `tooling/shell` | os/exec | (function tool) | Shell 指令執行 |
| `tooling/mcp` | MCP SDK + subprocess | `port.ToolProvider` | MCP tool 建構 |
| `observe/debug` | stderr logging | (plugin) | Debug request dump |
| `observe/memory` | genai + compression | (future: `port.MemoryService`) | 記憶壓縮 |

### internal/shared/ — Shared Kernel

| 職責 | 說明 |
|------|------|
| **做什麼** | 放跨 component 共享的 events、entity IDs、value objects |
| **不做什麼** | 不放完整 entities、不放 services、不當 utils 垃圾桶 |
| **目前** | 只有一個 bounded context，此目錄為 placeholder |
| **未來** | Multi-agent 時放 agent 間通訊的 event types 和 state key 定義 |

---

## Dependency Direction Enforcement

可在 CI 中加入以下 grep-based 檢查：

```bash
# core/domain/ 不可 import 任何外部套件
grep -r '"github.com\|"google.golang.org' internal/core/domain/ && exit 1

# core/ 不可 import infra/
grep -r '"github.com/neokn/agenticsystem/internal/infra' internal/core/ && exit 1

# infra/ 的各 adapter 之間不可互相 import
# (每個 adapter 只能 import core/ 和外部 SDK)
```

---

## Screaming Architecture

> 一個新人看到這個目錄結構，應該立刻知道「這是一個 Agent 系統」，而不是「這是一個用 ADK 的 Go 專案」。

- `agents/` 目錄 screams "這裡有 agent 定義"
- `internal/core/domain/` screams "這是 agent 的核心概念"
- `internal/infra/tooling/shell/` screams "agent 可以執行 shell"
- `cmd/telegram/` screams "agent 可以透過 Telegram 互動"
