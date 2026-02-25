#!/usr/bin/env bash
set -euo pipefail

CODELENS_BIN="${CODELENS_BIN:-$HOME/.local/bin/codelens}"
HOOK_BIN="${HOOK_BIN:-$HOME/.local/bin/codelens-hook}"
PROJECT_PATH="${1:-$PWD}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

mkdir -p "$HOME/.claude" "$HOME/.codex" "$HOME/.config/opencode"

CLAUDE_SETTINGS="$HOME/.claude/settings.json"
if [ ! -f "$CLAUDE_SETTINGS" ]; then
  echo '{}' > "$CLAUDE_SETTINGS"
fi

tmp_json="$(mktemp)"
jq --arg enforce "$HOOK_BIN enforce" --arg hook "$HOOK_BIN memory" --arg b "$HOOK_BIN bash" --arg r "$HOOK_BIN read" --arg g "$HOOK_BIN glob" '
  .hooks = (.hooks // {}) |
  .hooks.PreToolUse = (
    ((.hooks.PreToolUse // [])
      | map(select(.hooks[0].command != $enforce and .hooks[0].command != $hook and .hooks[0].command != $b and .hooks[0].command != $r and .hooks[0].command != $g)))
    + [
        {"matcher":"*","hooks":[{"type":"command","command":$enforce}]},
        {"matcher":"*","hooks":[{"type":"command","command":$hook}]},
        {"matcher":"Bash","hooks":[{"type":"command","command":$b}]},
        {"matcher":"Read","hooks":[{"type":"command","command":$r}]},
        {"matcher":"Glob","hooks":[{"type":"command","command":$g}]}
      ]
  )
' "$CLAUDE_SETTINGS" > "$tmp_json"
mv "$tmp_json" "$CLAUDE_SETTINGS"

echo "Updated $CLAUDE_SETTINGS"

CODEX_CONFIG="$HOME/.codex/config.toml"
if [ ! -f "$CODEX_CONFIG" ]; then
  cat > "$CODEX_CONFIG" <<TOML
model = "gpt-5.3-codex"
TOML
fi

# Remove any existing codelens MCP section (and nested env block) to avoid stale project pinning.
tmp_codex="$(mktemp)"
awk '
  BEGIN { skip = 0 }
  /^[[:space:]]*\[mcp_servers\.codelens\][[:space:]]*$/ { skip = 1; next }
  {
    if (skip == 1) {
      if ($0 ~ /^[[:space:]]*\[[^]]+\][[:space:]]*$/) {
        skip = 0
      } else {
        next
      }
    }
    print
  }
' "$CODEX_CONFIG" > "$tmp_codex"
mv "$tmp_codex" "$CODEX_CONFIG"

cat >> "$CODEX_CONFIG" <<TOML

[mcp_servers.codelens]
command = "$CODELENS_BIN"
args = ["serve"]

[mcp_servers.codelens.env]
CODELENS_OLLAMA_MODEL = "nomic-embed-text"
CODELENS_OLLAMA_URL = "http://localhost:11434"
CODELENS_PROJECT = "$PROJECT_PATH"
TOML

echo "Updated $CODEX_CONFIG"

OPENCODE_CONFIG="$HOME/.config/opencode/opencode.json"
if [ ! -f "$OPENCODE_CONFIG" ]; then
  cat > "$OPENCODE_CONFIG" <<JSON
{
  "\$schema": "https://opencode.ai/config.json"
}
JSON
fi

tmp_open="$(mktemp)"
jq \
  --arg codelens "$CODELENS_BIN" \
  --arg project "$PROJECT_PATH" \
  --arg ollama "http://localhost:11434" \
  --arg model "nomic-embed-text" \
  --arg instr1 "~/.claude/CLAUDE.md" \
  --arg instr2 "$PROJECT_PATH/AGENTS.md" \
  '
  .instructions = (
    ((.instructions // []) + [$instr1, $instr2]) | unique
  ) |
  .mcp = (.mcp // {}) |
  .mcp.codelens = {
    "type": "local",
    "command": [
      "env",
      ("CODELENS_PROJECT=" + $project),
      ("CODELENS_OLLAMA_URL=" + $ollama),
      ("CODELENS_OLLAMA_MODEL=" + $model),
      $codelens,
      "serve"
    ]
  }
  ' "$OPENCODE_CONFIG" > "$tmp_open"
mv "$tmp_open" "$OPENCODE_CONFIG"
echo "Updated $OPENCODE_CONFIG"

echo "Done. Restart Claude/Codex/OpenCode clients to load new config."
