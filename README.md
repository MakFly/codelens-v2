# CodeLens

> Agentic memory & semantic search MCP server for Claude Code.
> Reduces token consumption by 70-90% during codebase exploration.

## How it works

3 complementary layers:

1. **MCP Shadow Tools** — `search_codebase`, `read_file_smart`, `propose_memory`, `publish_memory`, `recall` replace grep/read/glob natively
2. **Hook Interceptor** — `codelens-hook` intercepts Claude Code's native Bash/Read tool calls and redirects to semantic search
3. **CLAUDE.md** — Prompt-level instructions that prime the agent to prefer CodeLens tools

## Prerequisites

```bash
# Go 1.22+
go version

# Ollama with nomic-embed-text
ollama pull nomic-embed-text
ollama serve   # ensure running at localhost:11434
```

## Installation

```bash
# Clone and build
git clone https://github.com/yourusername/codelens-v2
cd codelens-v2
make build
make install   # copies to /usr/local/bin
```

## Quick Start

```bash
# 1. Index your project
cd /path/to/your/project
codelens index .

# 2. Check index stats
codelens stats

# 3. Test a search
codelens search "authentication logic"

# 4. Configure Claude Code
# Copy .claude/settings.json to your project (or ~/.claude/settings.json for global)
cp /path/to/codelens-v2/.claude/settings.json /path/to/your/project/.claude/settings.json

# 5. Open Claude Code — CodeLens MCP server starts automatically
claude
```

## Watcher Daemon

Run a background index refresher to keep search results up to date:

```bash
# Start daemon (writes PID/state/log in .codelens/)
codelens watcher start .

# Check status (JSON)
codelens watcher status .

# Stop daemon
codelens watcher stop .
```

State files:
- `.codelens/watcher.pid`
- `.codelens/watcher.state.json`
- `.codelens/watcher.log`

You can tune interval and paths:

```bash
codelens watcher start . --interval 10s --pid-file .codelens/watcher.pid
```

## MCP Tools

| Tool | Replaces | Token savings |
|------|----------|---------------|
| `search_codebase(query)` | grep + read | 80-95% |
| `read_file_smart(path, query)` | Read() | 60-90% |
| `propose_memory(insight, citations)` | nothing (new capability) | — |
| `publish_memory(proposal_id)` | nothing (new capability) | — |
| `recall(context)` | re-discovery | ~100% for known patterns |
| `index_status()` | ls + wc | — |

## Tool Timeout Policy

All MCP tools are executed with a hard timeout (default: `20s`).

- Env override: `CODELENS_TOOL_TIMEOUT=10s`
- On timeout, tools return a structured error payload (`error_type`, `tool`, `stage`, `retryable`, `suggested_next_actions`) so the agent can react automatically.

## Memory Workflow (HITL)

1. `recall("task context")` at the start of a task
2. `propose_memory(...)` for stable insights + citations
3. `publish_memory(...)` only after human review

`remember(...)` remains available as backward-compatible alias to `propose_memory(...)`.

## Global Claude + Codex Setup

Use the helper script to install global hooks + MCP config:

```bash
./scripts/install-global-config.sh /path/to/project
```

This updates:
- `~/.claude/settings.json` with `codelens-hook` entries (including memory reminder hook)
- `~/.codex/config.toml` with `mcp_servers.codelens` (if missing)

Enforcement behavior:
- Native `Read` / `Glob` / `Search` are blocked at session start.
- They are unblocked only after at least one CodeLens MCP tool call is attempted in the same session.

## Running Tests

```bash
# Unit tests (no Ollama required)
make test

# Token savings benchmark (requires Ollama + indexed project)
make bench
```

## Architecture

See [PLAN.md](./PLAN.md) for the full design specification.
