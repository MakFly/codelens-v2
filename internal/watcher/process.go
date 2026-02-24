package watcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	ProjectPath string
	DBPath      string
	OllamaURL   string
	OllamaModel string
	Interval    time.Duration
	Force       bool
	PIDFile     string
	StateFile   string
	LogFile     string
}

type Status struct {
	State  State  `json:"state"`
	Stale  bool   `json:"stale"`
	Reason string `json:"reason,omitempty"`
}

func Start(cfg Config) (int, error) {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if err := cleanupStaleState(cfg); err != nil {
		return 0, fmt.Errorf("cleanup stale state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.PIDFile), 0755); err != nil {
		return 0, err
	}
	if pid, err := readPID(cfg.PIDFile); err == nil {
		if IsAlive(pid) {
			return 0, fmt.Errorf("watcher already running with pid %d", pid)
		}
		_ = removePID(cfg.PIDFile)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0755); err != nil {
		return 0, err
	}
	logf, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return 0, err
	}
	defer logf.Close()

	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	args := []string{
		"watcher", "run",
		"--project", cfg.ProjectPath,
		"--db", cfg.DBPath,
		"--ollama-url", cfg.OllamaURL,
		"--ollama-model", cfg.OllamaModel,
		"--interval", cfg.Interval.String(),
		"--pid-file", cfg.PIDFile,
		"--state-file", cfg.StateFile,
		"--log-file", cfg.LogFile,
	}
	if cfg.Force {
		args = append(args, "--force")
	}

	cmd := exec.Command(exe, args...)
	cmd.Dir = cfg.ProjectPath
	cmd.Env = os.Environ()
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	if err := writePID(cfg.PIDFile, pid); err != nil {
		_ = cmd.Process.Kill()
		return 0, err
	}
	_ = cmd.Process.Release()
	return pid, nil
}

func Stop(cfg Config, timeout time.Duration) error {
	pid, err := readPID(cfg.PIDFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !IsAlive(pid) {
		_ = removePID(cfg.PIDFile)
		state, _ := safeLoadState(cfg.StateFile)
		state.Running = false
		state.PID = 0
		_ = saveState(cfg.StateFile, state)
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsAlive(pid) {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if IsAlive(pid) {
		_ = proc.Signal(syscall.SIGKILL)
	}

	_ = removePID(cfg.PIDFile)
	state, _ := safeLoadState(cfg.StateFile)
	state.Running = false
	state.PID = 0
	_ = saveState(cfg.StateFile, state)
	return nil
}

func GetStatus(cfg Config) (Status, error) {
	st := Status{}
	state, err := safeLoadState(cfg.StateFile)
	if err != nil {
		return st, err
	}
	st.State = state

	pid, pidErr := readPID(cfg.PIDFile)
	if pidErr == nil {
		st.State.PID = pid
		st.State.Running = IsAlive(pid)
		if !st.State.Running {
			st.Stale = true
			st.Reason = "pid file exists but process is not running"
		}
		return st, nil
	}

	st.State.Running = false
	if !errors.Is(pidErr, os.ErrNotExist) {
		st.Stale = true
		st.Reason = pidErr.Error()
	}
	return st, nil
}

func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func cleanupStaleState(cfg Config) error {
	state, err := loadState(cfg.StateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if state.Running && state.PID > 0 && !IsAlive(state.PID) {
		state.Running = false
		state.PID = 0
		return saveState(cfg.StateFile, state)
	}
	return nil
}

func safeLoadState(stateFile string) (State, error) {
	state, err := loadState(stateFile)
	if err == nil {
		return state, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	return State{}, err
}

func Run(ctx context.Context, cfg Config, runOnce func(context.Context, Config) (int, int, error)) error {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if err := cleanupStaleState(cfg); err != nil {
		return fmt.Errorf("cleanup stale state: %w", err)
	}
	if err := writePID(cfg.PIDFile, os.Getpid()); err != nil {
		return err
	}
	defer func() { _ = removePID(cfg.PIDFile) }()

	var cycleMu sync.Mutex
	var cycleRunning bool

	state := State{
		Running:   true,
		PID:       os.Getpid(),
		Project:   cfg.ProjectPath,
		StartedAt: time.Now(),
		Interval:  cfg.Interval.String(),
	}
	if err := saveState(cfg.StateFile, state); err != nil {
		return err
	}

	update := func(files, chunks int, cycleErr error, started time.Time) {
		state.Running = true
		state.PID = os.Getpid()
		state.LastCycleAt = time.Now()
		state.LastCycleDurationMS = time.Since(started).Milliseconds()
		state.LastIndexedFiles = files
		state.LastIndexedChunks = chunks
		if cycleErr != nil {
			state.LastError = cycleErr.Error()
		} else {
			state.LastError = ""
		}
		_ = saveState(cfg.StateFile, state)
	}

	runCycle := func() {
		cycleMu.Lock()
		if cycleRunning {
			cycleMu.Unlock()
			return
		}
		cycleRunning = true
		cycleMu.Unlock()

		defer func() {
			cycleMu.Lock()
			cycleRunning = false
			cycleMu.Unlock()
		}()

		started := time.Now()
		files, chunks, err := runOnce(ctx, cfg)
		update(files, chunks, err, started)
	}

	cycleCfg := cfg
	started := time.Now()
	files, chunks, err := runOnce(ctx, cycleCfg)
	update(files, chunks, err, started)
	cycleCfg.Force = false

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			state.Running = false
			state.PID = 0
			_ = saveState(cfg.StateFile, state)
			return ctx.Err()
		case <-ticker.C:
			go runCycle()
		}
	}
}
