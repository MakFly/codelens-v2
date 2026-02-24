# CodeLens

<p align="center">
  <strong>Agentic memory & semantic search MCP server for Claude Code</strong><br>
  Reduces token consumption by 70-90% during codebase exploration
</p>

<p align="center">
  <a href="https://github.com/MakFly/codelens-v2/stargazers">
    <img src="https://img.shields.io/github/stars/MakFly/codelens-v2?style=flat" alt="stars">
  </a>
  <a href="https://github.com/MakFly/codelens-v2/releases">
    <img src="https://img.shields.io/github/v/release/MakFly/codelens-v2" alt="release">
  </a>
  <a href="https://github.com/MakFly/codelens-v2/blob/main/LICENSE">
    <img src="https://img.shields.io/github/license/MakFly/codelens-v2" alt="license">
  </a>
</p>

---

## Why CodeLens?

When working with Claude Code on large codebases, traditional exploration burns through tokens fast:

- `grep -r "auth" ./src` → reads thousands of lines across dozens of files
- `Read("src/Controller/UserController.php")` → loads entire 500-line files
- Re-discovering architecture patterns every session → wasteful repetition

**CodeLens replaces these with semantic search that understands code:**

```
┌─────────────────────────────────────────────────────────────┐
│  Traditional (grep + read)     │  CodeLens (semantic)     │
├─────────────────────────────────────────────────────────────┤
│  ~15,000 tokens                 │  ~400-800 tokens         │
│  Full file reads                │  Relevant chunks only    │
│  No memory                      │  Persistent insights    │
└─────────────────────────────────────────────────────────────┘
```

## How It Works

Three complementary layers:

1. **MCP Shadow Tools** — `search_codebase`, `read_file_smart`, `propose_memory`, `publish_memory`, `recall` replace grep/read/glob natively
2. **Hook Interceptor** — `codelens-hook` intercepts Claude Code's native Bash/Read tool calls and redirects to semantic search
3. **CLAUDE.md** — Prompt-level instructions that prime the agent to prefer CodeLens tools

## Installation

### Quick Install (curl)

```bash
# Install latest version
curl -sL https://github.com/MakFly/codelens-v2/releases/latest/download/install.sh | sh

# Or with specific version
VERSION=0.2.0 curl -sL https://github.com/MakFly/codelens-v2/releases/latest/download/install.sh | sh
```

### Homebrew

```bash
# First, tap the repository
brew tap MakFly/codelens

# Then install
brew install codelens
```

### From Source

```bash
# Clone and build
git clone https://github.com/MakFly/codelens-v2
cd codelens-v2

# Build with Go
go build -o codelens ./cmd/codelens/
go build -o codelens-hook ./cmd/hook/

# Install to PATH
mkdir -p ~/.local/bin
cp codelens codelens-hook ~/.local/bin/
export PATH=$HOME/.local/bin:$PATH
```

---

## Quick Start

```bash
# 1. Install CodeLens (see Installation section above)

# 2. Start Ollama (for embeddings)
ollama serve
ollama pull nomic-embed-text

# 3. Index your project
codelens index .

# 4. Check index stats
codelens stats

# 5. Start the watcher (optional, automatic)
codelens watcher start .

# 6. Restart your AI client
claude  # or: opencode, gemini, codex
```

### Supported AI Clients (auto-detected)

The installer automatically detects and configures:

| Client | Config File | MCP | Hooks |
|--------|-------------|-----|-------|
| Claude Code | `~/.claude/settings.json` | ✅ | ✅ |
| OpenCode | `~/.config/opencode/opencode.json` | ✅ | ❌ |
| Codex | `~/.codex/config.toml` | ✅ | ❌ |
| Gemini CLI | `~/.gemini/settings.json` | ✅ | ❌ |

If a client is not installed, it will be skipped automatically.

### Manual Installation (from release archives)

If you prefer to download binaries manually:

```bash
# Download for your platform
curl -sL https://github.com/MakFly/codelens-v2/releases/latest/download/codelens-darwin-arm64.tar.gz | tar -xz
# or
curl -sL https://github.com/MakFly/codelens-v2/releases/latest/download/codelens-linux-amd64.tar.gz | tar -xz

# Install to PATH
mkdir -p ~/.local/bin
cp codelens codelens-hook ~/.local/bin/
export PATH=$HOME/.local/bin:$PATH

# Index your project
codelens index .
```

## MCP Tools

| Tool | Replaces | Token Savings |
|------|----------|---------------|
| `search_codebase(query)` | grep + read | 80-95% |
| `read_file_smart(path, query)` | Read() | 60-90% |
| `propose_memory(insight, citations)` | — (new) | — |
| `publish_memory(proposal_id)` | — (new) | — |
| `recall(context)` | re-discovery | ~100% |
| `index_status()` | ls + wc | — |

### search_codebase

Semantic search over your codebase:

```typescript
// Call from Claude Code
const results = await search_codebase({
  query: "authentication logic",
  top_k: 5,
  language: "php"
});

// Returns relevant code chunks with line numbers
```

### read_file_smart

Smart file reading that returns only relevant sections:

```typescript
const content = await read_file_smart({
  path: "src/Controller/AuthController.php",
  query: "login validation"
});

// For large files: returns relevant chunks + structural outline
// For small files: returns full content
```

### Memory Workflow (Human-in-the-Loop)

```typescript
// 1. At start of any non-trivial task
const memories = await recall("working on payments feature");

// 2. After discovering a pattern
await propose_memory({
  insight: "Database transactions always go through TransactionManager",
  citations: [
    { file: "src/DB/TransactionManager.php", line_start: 10, line_end: 50 }
  ]
});

// 3. After human review (via CodeLens UI or CLI)
await publish_memory({ proposal_id: "prop_abc123" });
```

## Watcher Daemon

Keep your index up-to-date automatically with real-time file system watching:

```bash
# Start daemon (runs in background)
codelens watcher start .

# Check status
codelens watcher status .

# Stop daemon
codelens watcher stop .
```

### Features

- **Real-time file watching** — Uses `fsnotify` for immediate reactivity (no polling)
- **Concurrent cycle protection** — Mutex prevents overlapping index cycles
- **Stale state recovery** — Automatically cleans up stale state after crashes or system restarts
- **File lock detection** — Skips files being actively modified by editors (Cursor, VSCode, etc.)

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CODELENS_SKIP_LOCK_CHECK` | `false` | Set to `1` to disable file lock detection |

## Architecture

```
┌─────────────────────────┐
│    Indexer + Watcher   │
│   (fsnotify + chunker) │
└─────────────────────────┘
```

### Tech Stack

- **Go 1.22+** — Single binary, no runtime dependencies
- **MCP** — Model Context Protocol for agent communication
- **Ollama** — Local embeddings (`nomic-embed-text`)
- **SQLite** — Persistent storage for chunks and memories
- **HNSW** — In-memory vector index for semantic search
- **tree-sitter** — AST-aware code chunking (PHP, TypeScript, Go, Python)
- **fsnotify** — Real-time file system watching

## Running Tests

```bash
# Unit tests (no Ollama required)
make test

# Token savings benchmark (requires Ollama + indexed project)
make bench
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CODELENS_DB_PATH` | `.codelens/index.db` | SQLite database path |
| `CODELENS_OLLAMA_URL` | `http://localhost:11434` | Ollama server URL |
| `CODELENS_OLLAMA_MODEL` | `nomic-embed-text` | Embedding model |
| `CODELENS_TOOL_TIMEOUT` | `20s` | MCP tool timeout |
| `CODELENS_SKIP_LOCK_CHECK` | `false` | Skip file lock detection (set to `1`) |
| `CODELENS_MEMORY_AUTO_PUBLISH` | `true` | Auto-publish memories without manual review |
| `CODELENS_SNIPPET_CHARS` | `700` | Max characters per search result snippet |

### YAML Config

Edit `config/default.yaml`:

```yaml
ollama:
  url: "http://localhost:11434"
  model: "nomic-embed-text"

indexer:
  chunk_size: 150
  overlap: 3

watcher:
  interval: 30s
  exclude:
    - "*.git/*"
    - "node_modules/*"
    - "vendor/*"
```

## Troubleshooting

### "command not found: codelens"

Add to your shell profile (`~/.zshrc` or `~/.bashrc`):
```bash
export PATH=$HOME/.local/bin:$PATH
```

Then restart your shell or run: `source ~/.zshrc`

### "MCP server failed to start"

Check that:
1. Ollama is running: `ollama serve`
2. The model is installed: `ollama list` (should show `nomic-embed-text`)
3. The database exists: `ls -la .codelens/index.db`

Try starting manually to see errors:
```bash
codelens serve
```

### "No results from search_codebase"

Re-index your project:
```bash
codelens index . --force
```

Check index stats:
```bash
codelens stats
```

### Watcher not updating index

Check watcher status:
```bash
codelens watcher status .
```

Check watcher logs:
```bash
tail -f .codelens/watcher.log
```

### Claude Code not using CodeLens tools

Verify settings.json is correct:
```bash
cat ~/.claude/settings.json | jq '.mcpServers'
```

Should show:
```json
{
  "codelens": {
    "command": "/home/user/.local/bin/codelens",
    "args": ["serve"],
    ...
  }
}
```

### File lock errors

If you see file lock errors, you can disable lock checking:
```bash
CODELENS_SKIP_LOCK_CHECK=1 codelens index .
```

## License

MIT License — see [LICENSE](LICENSE) for details.
