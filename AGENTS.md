# CodeLens — Codex Agent Instructions

## MCP Memory Workflow (Required)

1. At the start of any non-trivial task, call:
   - `recall("<task context>")`
2. During exploration, rely on:
   - `search_codebase(query)`
   - `read_file_smart(path, query)`
3. When you discover a stable convention/pattern, create a proposal:
   - `propose_memory(insight, memory_type, citations)`
4. Publish only after explicit human review:
   - `publish_memory(proposal_id)`

## Guardrails

- Do not auto-publish memory.
- Always include citations for memory proposals.
- Prefer memory proposals for reusable insights only (not temporary debugging notes).
- Never finish an analysis/refactor answer without creating at least one `propose_memory(...)` and returning its `proposal_id`.
