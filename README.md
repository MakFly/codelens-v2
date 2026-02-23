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

## Quick Start

```bash
# 1. Clone and build
git clone https://github.com/MakFly/codelens-v2
cd codelens-v2
make build
make install

# 2. Start Ollama (for embeddings)
ollama serve
ollama pull nomic-embed-text

# 3. Index your project
cd /path/to/your/project
codelens index .

# 4. Check index stats
codelens stats

# 5. Configure Claude Code
cp /path/to/codelens-v2/.claude/settings.json /path/to/your/project/.claude/settings.json

# 6. Open Claude Code — CodeLens starts automatically
claude
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

Keep your index up-to-date automatically:

```bash
# Start daemon (runs in background)
codelens watcher start .

# Check status
codelens watcher status .

# Stop daemon
codelens watcher stop .
```

## Architecture

```
┌─────────────────────────┐
│   Claude Code / Agent  │
└───────────┬─────────────┘
            │ MCP Protocol
┌───────────▼─────────────┐
│   CodeLens MCP Server  │
│  - search_codebase()   │
│  - read_file_smart()  │
│  - remember/recall()   │
└───────────┬─────────────┘
            │
    ┌───────┴───────┐
    ▼               ▼
┌────────┐    ┌────────────┐
│ Vector │    │   Memory   │
│ Index  │    │   Store    │
│ (HNSW) │    │  (SQLite)  │
└────────┘    └────────────┘
    │               │
    └───────┬───────┘
            ▼
┌─────────────────────────┐
│    Indexer + Watcher    │
│   (tree-sitter chunker) │
└─────────────────────────┘
```

### Tech Stack

- **Go 1.22+** — Single binary, no runtime dependencies
- **MCP** — Model Context Protocol for agent communication
- **Ollama** — Local embeddings (`nomic-embed-text`)
- **SQLite** — Persistent storage for chunks and memories
- **HNSW** — In-memory vector index for semantic search
- **tree-sitter** — AST-aware code chunking (PHP, TypeScript, Go, Python)

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

## License

MIT License — see [LICENSE](LICENSE) for details.
