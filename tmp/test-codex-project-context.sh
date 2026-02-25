#!/usr/bin/env bash
set -euo pipefail

MODE="${MODE:-non-interactive}"
CODEX_BIN="${CODEX_BIN:-codex}"
TARGET_DIR="${1:-$PWD}"
EXPECTED_PROJECT="${2:-$TARGET_DIR}"

if ! command -v "$CODEX_BIN" >/dev/null 2>&1; then
  echo "codex binary not found: $CODEX_BIN" >&2
  exit 2
fi

if [ ! -d "$TARGET_DIR" ]; then
  echo "target dir does not exist: $TARGET_DIR" >&2
  exit 2
fi

EXPECTED_PROJECT="$(cd "$EXPECTED_PROJECT" && pwd)"
TARGET_DIR="$(cd "$TARGET_DIR" && pwd)"

PROMPT='Run only mcp__codelens__index_status. In the final answer, include exactly one line that starts with "Project:" copied from tool output.'

tmp_out="$(mktemp)"
cleanup() {
  rm -f "$tmp_out"
}
trap cleanup EXIT

run_non_interactive() {
  "$CODEX_BIN" exec --yolo \
    --cd "$TARGET_DIR" \
    --output-last-message "$tmp_out" \
    "$PROMPT"
}

run_interactive_best_effort() {
  # Run non-interactive `exec` inside a PTY so we validate behavior in a terminal-like session too.
  if ! command -v script >/dev/null 2>&1; then
    echo "FAILED: interactive mode requires \`script\` command for PTY emulation" >&2
    return 1
  fi
  timeout 90s script -q -e -c \
    "$CODEX_BIN exec --yolo --cd \"$TARGET_DIR\" --output-last-message \"$tmp_out\" \"$PROMPT\"" \
    /dev/null >/dev/null 2>&1 || true
}

case "$MODE" in
  non-interactive)
    run_non_interactive
    ;;
  interactive)
    run_interactive_best_effort
    ;;
  *)
    echo "invalid MODE: $MODE (expected: non-interactive|interactive)" >&2
    exit 2
    ;;
esac

project_line="$(grep -E '^Project:[[:space:]]+' "$tmp_out" | head -n1 || true)"
if [ -z "$project_line" ]; then
  project_line="$(grep -E 'Project:[[:space:]]+' "$tmp_out" | head -n1 || true)"
fi

if [ -z "$project_line" ]; then
  echo "FAILED: could not find a Project line in Codex output" >&2
  sed -n '1,120p' "$tmp_out" >&2
  exit 1
fi

actual_project="$(echo "$project_line" | sed -E 's/.*Project:[[:space:]]*//')"
if [ "$actual_project" != "$EXPECTED_PROJECT" ]; then
  echo "FAILED: project mismatch" >&2
  echo "expected: $EXPECTED_PROJECT" >&2
  echo "actual:   $actual_project" >&2
  exit 1
fi

echo "PASS: Codex/CodeLens project context is correct"
echo "Project: $actual_project"
