# Temporary Codex Context Checks

This folder contains temporary verification scripts for Codex + CodeLens project routing.

## Test: `test-codex-project-context.sh`

Verifies that a Codex session resolves CodeLens tools (`index_status`) to the expected project.

### Non-interactive (`codex exec --yolo`)

```bash
./tmp/test-codex-project-context.sh /path/to/repo /path/to/repo
```

### Interactive best-effort (`codex --yolo`)

```bash
MODE=interactive ./tmp/test-codex-project-context.sh /path/to/repo /path/to/repo
```

Notes:
- Interactive mode is wrapped in a timeout and may be less deterministic.
- This is a temporary test harness; it is not wired into CI by default.
