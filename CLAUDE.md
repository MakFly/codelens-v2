# CodeLens — Agent Instructions

## Tool Priority — MANDATORY

Before ANY file operation, use CodeLens MCP tools:

| Situation | Tool | NEVER use |
|-----------|------|-----------|
| Finding auth, service, repository logic | `search_codebase("...")` | `Bash(grep)`, `Glob` |
| Starting work on a feature | `recall("feature context")` | Nothing — just do it |
| Reading a file > 200 lines | `read_file_smart(path)` | `Read(path)` |
| Discovered a pattern/convention | `propose_memory(insight, citations)` | — |
| Human approved proposal | `publish_memory(proposal_id)` | Auto-publish |
| Unsure what's indexed | `index_status()` | — |

## Tool Usage Examples

```
# WRONG ❌
grep -r "AuthService" ./src
Read("src/Services/AuthService.php")

# RIGHT ✅
search_codebase("authentication service login logic")
read_file_smart("src/Services/AuthService.php", query="login")
```

## Why This Matters

- `search_codebase` → returns ~500 tokens vs 15,000+ for grep+read
- `recall` → injects institutional knowledge vs re-discovering it every session
- `read_file_smart` → returns relevant sections vs entire 2000-line files

## When grep/Read is OK

- System files outside the project (e.g., `/etc/`, `/usr/`)
- Binary files
- Files explicitly requested by the user by exact path
- After `search_codebase` returns 0 results AND you've retried with a different query
