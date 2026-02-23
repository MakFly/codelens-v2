package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestIsNativeReadSearchTool(t *testing.T) {
	if !isNativeReadSearchTool("read") || !isNativeReadSearchTool("glob") || !isNativeReadSearchTool("search") {
		t.Fatal("expected read/glob/search to be native read/search tools")
	}
	if isNativeReadSearchTool("bash") {
		t.Fatal("bash should not be native read/search tool")
	}
}

func TestIsCodeLensMCPTool(t *testing.T) {
	if !isCodeLensMCPTool("mcp__codelens__search_codebase") {
		t.Fatal("expected mcp__codelens__* to be detected")
	}
	if !isCodeLensMCPTool("codelens.recall") {
		t.Fatal("expected codelens.* to be detected")
	}
	if isCodeLensMCPTool("mcp__other__tool") {
		t.Fatal("other mcp tool should not be detected as codelens")
	}
}

func TestEnforceHook_BlocksNativeBeforeMCP(t *testing.T) {
	home := t.TempDir()
	payload := map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": "x.php"},
		"session_id": "s1",
	}
	code, out := runHookProcess(t, "enforce", payload, home)
	if code != 2 {
		t.Fatalf("expected exit 2 block, got %d output=%s", code, out)
	}
	if !strings.Contains(out, "CodeLens enforcement") {
		t.Fatalf("expected enforcement message, got %s", out)
	}
}

func TestEnforceHook_AllowsNativeAfterMCPAttempt(t *testing.T) {
	home := t.TempDir()
	mcpPayload := map[string]interface{}{
		"tool_name":  "mcp__codelens__search_codebase",
		"tool_input": map[string]interface{}{},
		"session_id": "s2",
	}
	code, out := runHookProcess(t, "enforce", mcpPayload, home)
	if code != 0 {
		t.Fatalf("expected mcp attempt to pass, got %d output=%s", code, out)
	}

	readPayload := map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": "x.php"},
		"session_id": "s2",
	}
	code, out = runHookProcess(t, "enforce", readPayload, home)
	if code != 0 {
		t.Fatalf("expected native read after mcp attempt to pass, got %d output=%s", code, out)
	}
}

func runHookProcess(t *testing.T, mode string, payload map[string]interface{}, home string) (int, string) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHookProcess", "--", mode)
	cmd.Env = append(os.Environ(), "TEST_HOOK_PROCESS=1", "HOME="+home)
	cmd.Stdin = bytes.NewReader(b)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if err == nil {
		return 0, out.String()
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), out.String()
	}
	t.Fatalf("run helper process: %v", err)
	return -1, out.String()
}

func TestHookProcess(t *testing.T) {
	if os.Getenv("TEST_HOOK_PROCESS") != "1" {
		return
	}
	args := os.Args
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+1 >= len(args) {
		os.Exit(1)
	}
	os.Args = []string{"codelens-hook", args[sep+1]}
	main()
}
