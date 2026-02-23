package mcp

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestConfiguredToolTimeout_Default(t *testing.T) {
	_ = os.Unsetenv("CODELENS_TOOL_TIMEOUT")
	if got := configuredToolTimeout(); got != defaultToolTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultToolTimeout, got)
	}
}

func TestConfiguredToolTimeout_FromEnv(t *testing.T) {
	t.Setenv("CODELENS_TOOL_TIMEOUT", "5s")
	if got := configuredToolTimeout(); got != 5*time.Second {
		t.Fatalf("expected 5s, got %s", got)
	}
}

func TestWithToolTimeout_RespectsParentSoonerDeadline(t *testing.T) {
	t.Setenv("CODELENS_TOOL_TIMEOUT", "20s")
	parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ctx, stop, meta := withToolTimeout(parent, "search_codebase")
	defer stop()
	if meta.Timeout > 2*time.Second {
		t.Fatalf("expected timeout capped by parent, got %s", meta.Timeout)
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("expected deadline in child context")
	}
}
