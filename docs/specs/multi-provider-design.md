# Multi-Provider AI Support: Architecture Design

## Overview

Добавляем Codex CLI как полноценный AI-провайдер наравне с Claude CLI.
Переключение через конфиг. Claude-путь остаётся нетронутым.

---

## Go Backend

### 1. Новый пакет `internal/codex/`

#### `resolve.go` — binary discovery (аналог `internal/claude/resolve.go`):
- `FindBinary(override string) string` — ищет `codex` бинарник (PATH → login shell → fallback dirs)
- `RichPATH() string` — обогащённый PATH для subprocess
- Кэширование через `sync.Once`

#### `generator.go` — CodexGenerator реализует `digest.Generator`:
```go
type CodexGenerator struct {
    model     string
    codexPath string
}
func NewCodexGenerator(model, codexPath string) *CodexGenerator
func (g *CodexGenerator) Generate(ctx context.Context, systemPrompt, userMessage, sessionID string) (string, *digest.Usage, string, error)
```

CLI вызов:
```
codex exec \
  --model <model> \
  --json \
  -c approval_policy=never \
  -c sandbox_mode=read-only \
  -c developer_instructions="<systemPrompt>" \
  "<userMessage>"
```

Ключевое:
- System prompt через `-c developer_instructions="..."` (НЕ конкатенация в userMessage)
- `--json` → JSONL output
- `--ephemeral` для отключения сессий

#### `client.go` — CodexClient для streaming (ask/chat):
```go
type Client struct {
    model    string
    dbPath   string
    codexCmd string
}
func NewClient(model, dbPath, codexPath string) *Client
func (c *Client) Query(ctx context.Context, systemPrompt, userMessage, sessionID string) (<-chan string, <-chan error, <-chan string)
func (c *Client) QuerySync(ctx context.Context, systemPrompt, userMessage, sessionID string) (string, *ai.Usage, error)
```

#### `models.go` — модели Codex:
```go
const (
    ModelDefault     = "gpt-5.4"        // аналог Sonnet
    ModelLightweight = "gpt-5.4-mini"   // аналог Haiku
)
func ModelForSource(source string) string // тот же маппинг что digest.ModelForSource
```

#### `parse.go` — парсинг JSONL events:
```go
type CodexEvent struct {
    Type     string      `json:"type"`      // thread.started, turn.started, turn.completed, item.started, item.completed, error
    ThreadID string      `json:"thread_id"`
    Item     *CodexItem  `json:"item"`
    Usage    *CodexUsage `json:"usage"`
    Error    *CodexError `json:"error"`
}
type CodexItem struct {
    ID      string `json:"id"`
    Type    string `json:"type"`    // agent_message, command_execution, mcp_tool_call
    Content string `json:"content"`
}
type CodexUsage struct {
    InputTokens  int `json:"input_tokens"`
    OutputTokens int `json:"output_tokens"`
}
type CodexError struct {
    Message string `json:"message"`
}
```

Извлечение результата: последний event `item.completed` с `item.type == "agent_message"` содержит финальный ответ.

#### `mcp.go` — MCP конфигурация для Codex:
- Создаём temp директорию с `.codex/config.toml` содержащую MCP конфиг для SQLite
- Используем `--cd` для указания этой директории
- Cleanup в defer

Формат `.codex/config.toml`:
```toml
[mcp_servers.sqlite]
command = "npx"
args = ["-y", "@anthropic-ai/mcp-server-sqlite", "<dbPath>"]
```

### 2. Интерфейс `ai.Provider`

Новый файл `internal/ai/provider.go`:
```go
type Provider interface {
    Query(ctx context.Context, systemPrompt, userMessage, sessionID string) (<-chan string, <-chan error, <-chan string)
    QuerySync(ctx context.Context, systemPrompt, userMessage, sessionID string) (string, *Usage, error)
}
```

`ai.Client` (Claude) уже реализует по сигнатурам. `codex.Client` — новая реализация.

### 3. Конфигурация

`internal/config/config.go` — добавить:
```go
// В AIConfig:
Provider string `mapstructure:"provider"` // "claude" (default) | "codex"

// В Config (корневой уровень, рядом с ClaudePath):
CodexPath string `mapstructure:"codex_path"`
```

`internal/config/defaults.go`:
```go
const DefaultAIProvider = "claude"
```

### 4. Factory-функции

`cmd/generator.go`:
```go
func cliGenerator(cfg *config.Config) digest.Generator {
    if cfg.AI.Provider == "codex" {
        return codex.NewCodexGenerator(codex.ModelDefault, cfg.CodexPath)
    }
    return digest.NewClaudeGenerator(digest.ModelSonnet, cfg.ClaudePath)
}
```

`cmd/ask.go` — `newAIClient(cfg, dbPath) ai.Provider`

### 5. CLI флаг

`cmd/root.go` — persistent flag `--provider` (claude|codex), override cfg.AI.Provider.

### 6. Go файлы (scope для Go Dev):
1. `internal/codex/resolve.go` — новый
2. `internal/codex/models.go` — новый
3. `internal/codex/generator.go` — новый
4. `internal/codex/client.go` — новый
5. `internal/codex/mcp.go` — новый
6. `internal/codex/parse.go` — новый
7. `internal/ai/provider.go` — новый
8. `internal/config/config.go` — изменить
9. `internal/config/defaults.go` — изменить
10. `cmd/generator.go` — изменить
11. `cmd/ask.go` — изменить
12. `cmd/root.go` — изменить

---

## Swift Desktop

### 1. CodexService.swift — новый сервис

Переименовать `ClaudeServiceProtocol` → `AIServiceProtocol`. Обновить:
- Объявление протокола
- `ClaudeService: AIServiceProtocol`
- `CodexService: AIServiceProtocol`
- Все ссылки в ViewModels и DI

```swift
final class CodexService: AIServiceProtocol, Sendable {
    func stream(
        prompt: String,
        systemPrompt: String?,
        sessionID: String?,
        dbPath: String?,
        model: String?,
        extraAllowedTools: [String]
    ) -> AsyncThrowingStream<StreamEvent, Error>
}
```

CLI вызов:
```
codex exec \
  --model <model> \
  --json \
  -c approval_policy=never \
  -c sandbox_mode=read-only \
  -c developer_instructions="<systemPrompt>" \
  --cd <workingDir> \
  "<prompt>"
```

Ключевое:
- System prompt через `-c developer_instructions="..."` (НЕ конкатенация)
- `--json` → JSONL output, парсим построчно
- `item.completed` + `type == "agent_message"` → `.turnComplete(content)`
- Streaming deltas через `item.started` → `.text(delta)`
- `turn.completed` → можно игнорировать
- Конец процесса → `.done`

MCP для SQLite: создаём temp `.codex/config.toml`:
```toml
[mcp_servers.sqlite]
command = "npx"
args = ["-y", "@anthropic-ai/mcp-server-sqlite", "<dbPath>"]
```
Записываем в temp dir, используем `--cd`.

Binary discovery: `Constants.findCodexPath()` — аналог `findClaudePath()`.

### 2. ConfigService.swift — провайдер в настройках

Новые поля:
```swift
var aiProvider: String?  // "claude" | "codex", default "claude"
var codexPath: String?
```

В `reload()`: читать из yaml `ai.provider` и `codex_path`.
В `save()`: сохранять обратно.

### 3. ChatViewModel.swift — модели и провайдер

```swift
enum AIProvider: String, CaseIterable, Identifiable {
    case claude
    case codex
    var id: String { rawValue }
    var displayName: String {
        switch self {
        case .claude: "Claude"
        case .codex: "Codex"
        }
    }
}

enum ChatModel: String, CaseIterable, Identifiable {
    // Claude
    case sonnet = "claude-sonnet-4-6"
    case haiku = "claude-haiku-4-5-20251001"
    case opus = "claude-opus-4-6"
    // Codex
    case gpt54 = "gpt-5.4"
    case gpt54mini = "gpt-5.4-mini"
    case gpt53codex = "gpt-5.3-codex"

    var provider: AIProvider { ... }
    var displayName: String { ... }

    static func models(for provider: AIProvider) -> [ChatModel] {
        allCases.filter { $0.provider == provider }
    }
}
```

ChatView: picker показывает только модели текущего провайдера.

При смене провайдера:
```swift
let service: any AIServiceProtocol = switch provider {
    case .claude: ClaudeService()
    case .codex: CodexService()
}
```

### 4. Settings UI

В Settings → AI секции:
- **Provider picker**: `Picker("AI Provider", selection: $configService.aiProvider)` — Claude / Codex
- **Codex Path**: текстовое поле, показывается только когда provider == codex
- Model picker: фильтруется по текущему провайдеру

### 5. JSONL парсинг (Codex events)

```swift
struct CodexEvent: Decodable {
    let type: String
    let threadId: String?
    let item: CodexItem?
    let usage: CodexUsage?
    let error: CodexError?
}
struct CodexItem: Decodable {
    let id: String
    let type: String
    let content: String?
}
struct CodexUsage: Decodable {
    let inputTokens: Int
    let outputTokens: Int
}
struct CodexError: Decodable {
    let message: String
}
```

### 6. Swift файлы (scope для Swift Dev):
1. `WatchtowerDesktop/Sources/Services/CodexService.swift` — новый
2. `WatchtowerDesktop/Sources/Services/ClaudeService.swift` — переименовать протокол → AIServiceProtocol
3. `WatchtowerDesktop/Sources/Services/Constants.swift` — findCodexPath()
4. `WatchtowerDesktop/Sources/Services/ConfigService.swift` — aiProvider, codexPath
5. `WatchtowerDesktop/Sources/ViewModels/ChatViewModel.swift` — ChatModel расширение, AIProvider enum
6. `WatchtowerDesktop/Sources/Views/SettingsView.swift` — provider picker

---

## Config YAML формат (Go ↔ Swift контракт)

```yaml
ai:
  provider: "codex"        # string: "claude" | "codex", default "claude"
  model: "gpt-5.4"         # string: модель текущего провайдера
  workers: 5               # int
codex_path: "/usr/local/bin/codex"  # string, optional
claude_path: ""                      # string, optional (уже есть)
```

## Codex JSONL event types (для парсинга в обоих клиентах)

- `thread.started` → `thread_id: string`
- `turn.started` → (no payload)
- `item.completed` → `item.id: string, item.type: string, item.content: string`
- `turn.completed` → `usage.input_tokens: int, usage.output_tokens: int`
- `error` → `error.message: string`

Go типы: `int` для tokens, `string` для IDs и content.
Swift типы: `Int` для tokens, `String` для IDs и content.
