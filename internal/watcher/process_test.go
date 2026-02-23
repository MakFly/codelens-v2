package watcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "watcher.state.json")
	in := State{Running: true, PID: 1234, Project: "/tmp/proj", Interval: "5s"}
	if err := saveState(statePath, in); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	out, err := loadState(statePath)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if out.PID != 1234 || !out.Running || out.Project != in.Project {
		t.Fatalf("unexpected state: %+v", out)
	}
}

func TestIsAlive(t *testing.T) {
	if !IsAlive(os.Getpid()) {
		t.Fatal("current process should be alive")
	}
}

func TestRunUpdatesStateAndStops(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{
		ProjectPath: tmp,
		PIDFile:     filepath.Join(tmp, "watcher.pid"),
		StateFile:   filepath.Join(tmp, "watcher.state.json"),
		LogFile:     filepath.Join(tmp, "watcher.log"),
		Interval:    10 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()

	err := Run(ctx, cfg, func(context.Context, Config) (int, int, error) {
		calls++
		return 1, 2, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if calls == 0 {
		t.Fatal("runOnce was never called")
	}

	state, err := loadState(cfg.StateFile)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if state.Running {
		t.Fatalf("watcher should not be running: %+v", state)
	}
	if _, err := os.Stat(cfg.PIDFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pid file should be removed, got err=%v", err)
	}
}
