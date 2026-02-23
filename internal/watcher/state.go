package watcher

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// State stores watcher runtime status on disk.
type State struct {
	Running             bool      `json:"running"`
	PID                 int       `json:"pid"`
	Project             string    `json:"project"`
	StartedAt           time.Time `json:"started_at,omitempty"`
	LastCycleAt         time.Time `json:"last_cycle_at,omitempty"`
	LastCycleDurationMS int64     `json:"last_cycle_duration_ms,omitempty"`
	LastIndexedFiles    int       `json:"last_indexed_files,omitempty"`
	LastIndexedChunks   int       `json:"last_indexed_chunks,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
	Interval            string    `json:"interval"`
}

func loadState(path string) (State, error) {
	var s State
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	return s, nil
}

func saveState(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writePID(pidPath string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(pidPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(pidPath, []byte(([]byte)(fmtInt(pid))), 0644)
}

func readPID(pidPath string) (int, error) {
	b, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	pid, err := parseInt(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, errors.New("invalid pid file")
	}
	if pid <= 0 {
		return 0, errors.New("invalid pid value")
	}
	return pid, nil
}

func removePID(pidPath string) error {
	err := os.Remove(pidPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
