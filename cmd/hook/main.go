package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Claude Code PreToolUse hook binary.
// Usage: codelens-hook <tool_type>
// Reads JSON from stdin, writes JSON or exits 0 (passthrough) / 2 (block with result).

// HookInput is the JSON structure Claude Code sends to PreToolUse hooks.
type HookInput struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	SessionID string          `json:"session_id"`
}

// BashInput is the tool_input for Bash tool.
type BashInput struct {
	Command string `json:"command"`
}

// ReadInput is the tool_input for Read tool.
type ReadInput struct {
	FilePath string `json:"file_path"`
}

// HookOutput is returned when we want to block + provide custom result.
type HookOutput struct {
	Action string `json:"action"` // "block"
	Reason string `json:"reason"`
	Result string `json:"result"`
}

type memorySessionState struct {
	PromptedAt          string `json:"prompted_at,omitempty"`
	RecalledAt          string `json:"recalled_at,omitempty"`
	MCPAttemptedAt      string `json:"mcp_attempted_at,omitempty"`
	AllowNativeFallback bool   `json:"allow_native_fallback,omitempty"`
}

type memoryStateFile struct {
	Sessions map[string]memorySessionState `json:"sessions"`
}

// Patterns that indicate codebase search commands we should intercept.
var (
	grepPattern = regexp.MustCompile(`\b(grep|rg|ripgrep)\b`)
	findPattern = regexp.MustCompile(`\b(find|fd)\b.*\.(php|ts|js|go|py|tsx|jsx)`)
	catPattern  = regexp.MustCompile(`\b(cat|less|head|tail)\b.*\.(php|ts|js|go|py|tsx|jsx)`)
)

const largeFileThreshold = 400 // lines

func main() {
	if len(os.Args) < 2 {
		os.Exit(0) // passthrough
	}
	toolType := os.Args[1]

	var input HookInput
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		os.Exit(0) // passthrough on parse error
	}

	switch toolType {
	case "enforce":
		handleEnforce(input)
	case "bash":
		handleBash(input)
	case "read":
		handleRead(input)
	case "glob":
		handleGlob(input)
	case "memory":
		handleMemory(input)
	default:
		os.Exit(0)
	}
}

func handleEnforce(input HookInput) {
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		sessionID = "default"
	}
	now := time.Now().UTC()

	state, path := loadMemoryState()
	if state.Sessions == nil {
		state.Sessions = map[string]memorySessionState{}
	}
	s := state.Sessions[sessionID]

	toolName := strings.ToLower(strings.TrimSpace(input.ToolName))
	if isCodeLensMCPTool(toolName) {
		s.MCPAttemptedAt = now.Format(time.RFC3339)
		state.Sessions[sessionID] = s
		_ = saveMemoryState(path, state)
		os.Exit(0)
	}

	if !isNativeReadSearchTool(toolName) {
		os.Exit(0)
	}

	if s.AllowNativeFallback || s.MCPAttemptedAt != "" {
		os.Exit(0)
	}

	blockWithMessage("⚠ CodeLens enforcement.\nUse MCP tools first: `search_codebase(...)` then `read_file_smart(...)`.\nNative Read/Glob/Search are blocked until at least one CodeLens MCP tool is attempted in this session.")
}

func handleBash(input HookInput) {
	var bash BashInput
	if err := json.Unmarshal(input.ToolInput, &bash); err != nil {
		os.Exit(0)
	}

	cmd := bash.Command

	// Intercept grep/rg searching the project
	if grepPattern.MatchString(cmd) && !isSystemCommand(cmd) {
		query := extractGrepQuery(cmd)
		if query != "" {
			blockWithMessage(fmt.Sprintf(
				"⚡ CodeLens intercepted grep command.\n"+
					"Use `search_codebase(%q)` instead — returns semantic results with ~90%% fewer tokens.\n\n"+
					"Original command: %s",
				query, cmd,
			))
			return
		}
	}

	// Intercept find/fd searching for source files
	if findPattern.MatchString(cmd) {
		blockWithMessage(fmt.Sprintf(
			"⚡ CodeLens intercepted find command.\n"+
				"Use `search_codebase()` with a conceptual query instead.\n"+
				"For file listing, use `index_status()` to see indexed files.\n\n"+
				"Original: %s",
			cmd,
		))
		return
	}

	// Intercept cat/head on source files
	if catPattern.MatchString(cmd) {
		filePath := extractFilePath(cmd)
		if filePath != "" {
			blockWithMessage(fmt.Sprintf(
				"⚡ CodeLens intercepted file read.\n"+
					"Use `read_file_smart(%q)` instead — returns only relevant sections.\n\n"+
					"Original: %s",
				filePath, cmd,
			))
			return
		}
	}

	os.Exit(0) // passthrough
}

func handleRead(input HookInput) {
	var read ReadInput
	if err := json.Unmarshal(input.ToolInput, &read); err != nil {
		os.Exit(0)
	}

	// Check if file is large
	lineCount, err := countLines(read.FilePath)
	if err != nil {
		os.Exit(0) // can't determine → passthrough
	}

	if lineCount >= largeFileThreshold {
		blockWithMessage(fmt.Sprintf(
			"⚡ CodeLens intercepted Read on large file (%d lines).\n"+
				"Use `read_file_smart(%q)` instead — returns only relevant sections (~%d lines estimated).\n"+
				"Add a `query` parameter to focus on specific functionality.",
			lineCount, read.FilePath, lineCount/5,
		))
		return
	}

	os.Exit(0) // passthrough for small files
}

func handleGlob(input HookInput) {
	// For Glob, we suggest using search_codebase for conceptual queries
	// but don't block — Glob is often used for legitimate file discovery
	// We just add a hint by exiting 0
	os.Exit(0)
}

func handleMemory(input HookInput) {
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		sessionID = "default"
	}
	now := time.Now().UTC()

	state, path := loadMemoryState()
	if state.Sessions == nil {
		state.Sessions = map[string]memorySessionState{}
	}
	s := state.Sessions[sessionID]

	toolName := strings.ToLower(strings.TrimSpace(input.ToolName))
	if strings.Contains(toolName, "recall") {
		s.RecalledAt = now.Format(time.RFC3339)
		state.Sessions[sessionID] = s
		_ = saveMemoryState(path, state)
		os.Exit(0)
	}

	if s.RecalledAt != "" {
		if recalledAt, err := time.Parse(time.RFC3339, s.RecalledAt); err == nil {
			if now.Sub(recalledAt) <= 12*time.Hour {
				os.Exit(0)
			}
		}
	}

	if s.PromptedAt == "" {
		s.PromptedAt = now.Format(time.RFC3339)
		state.Sessions[sessionID] = s
		_ = saveMemoryState(path, state)
		blockWithMessage("⚠ CodeLens memory reminder.\nBefore coding, call `recall(\"task context\")` to load validated team memory.\nThis reminder blocks once per session only.")
		return
	}

	os.Exit(0)
}

// --- Helpers ---

func blockWithMessage(msg string) {
	out := HookOutput{
		Action: "block",
		Reason: msg,
		Result: msg,
	}
	json.NewEncoder(os.Stdout).Encode(out)
	os.Exit(2) // Exit code 2 = block in Claude Code hooks
}

func extractGrepQuery(cmd string) string {
	// Try to extract the search pattern from grep/rg command
	// grep -r "pattern" ./src  →  "pattern"
	// rg "pattern"  →  "pattern"
	parts := strings.Fields(cmd)
	for i, p := range parts {
		if !strings.HasPrefix(p, "-") && i > 0 && !strings.HasPrefix(p, ".") && !strings.HasPrefix(p, "/") {
			// Strip quotes
			q := strings.Trim(p, `"'`)
			if len(q) > 1 {
				return q
			}
		}
	}
	return ""
}

var filePathPattern = regexp.MustCompile(`[\w./\-]+\.(php|ts|js|tsx|jsx|go|py)`)

func extractFilePath(cmd string) string {
	if m := filePathPattern.FindString(cmd); m != "" {
		return m
	}
	return ""
}

func isSystemCommand(cmd string) bool {
	systemPaths := []string{"/etc/", "/usr/", "/sys/", "/proc/", "/var/log/"}
	for _, p := range systemPaths {
		if strings.Contains(cmd, p) {
			return true
		}
	}
	return false
}

func isCodeLensMCPTool(toolName string) bool {
	return strings.Contains(toolName, "mcp__codelens__") || strings.Contains(toolName, "codelens.")
}

func isNativeReadSearchTool(toolName string) bool {
	switch toolName {
	case "read", "glob", "search":
		return true
	default:
		return false
	}
}

func countLines(filePath string) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	count := 0
	for {
		n, err := f.Read(buf)
		for _, b := range buf[:n] {
			if b == '\n' {
				count++
			}
		}
		if err != nil {
			break
		}
	}
	return count, nil
}

func loadMemoryState() (memoryStateFile, string) {
	state := memoryStateFile{Sessions: map[string]memorySessionState{}}
	home, err := os.UserHomeDir()
	if err != nil {
		return state, ".codelens-hook-memory.json"
	}
	path := filepath.Join(home, ".codelens", "hook-memory-state.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return state, path
	}
	_ = json.Unmarshal(b, &state)
	if state.Sessions == nil {
		state.Sessions = map[string]memorySessionState{}
	}
	return state, path
}

func saveMemoryState(path string, state memoryStateFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
