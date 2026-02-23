package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const defaultToolTimeout = 20 * time.Second

var errInvalidInput = errors.New("invalid_input")

type inputError struct {
	msg string
}

func (e inputError) Error() string { return e.msg }
func (e inputError) Unwrap() error { return errInvalidInput }

type toolMeta struct {
	Name    string
	Timeout time.Duration
	Started time.Time
}

type toolErrorPayload struct {
	ErrorType            string   `json:"error_type"`
	Tool                 string   `json:"tool"`
	TimeoutMS            int64    `json:"timeout_ms,omitempty"`
	Stage                string   `json:"stage"`
	Retryable            bool     `json:"retryable"`
	Message              string   `json:"message"`
	SuggestedNextActions []string `json:"suggested_next_actions"`
}

func withToolTimeout(ctx context.Context, toolName string) (context.Context, context.CancelFunc, toolMeta) {
	t := configuredToolTimeout()
	if dl, ok := ctx.Deadline(); ok {
		remaining := time.Until(dl)
		if remaining > 0 && remaining < t {
			t = remaining
		}
	}
	ctx2, cancel := context.WithTimeout(ctx, t)
	return ctx2, cancel, toolMeta{Name: toolName, Timeout: t, Started: time.Now()}
}

func configuredToolTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CODELENS_TOOL_TIMEOUT"))
	if raw == "" {
		return defaultToolTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultToolTimeout
	}
	return d
}

func invalidInputf(format string, args ...interface{}) error {
	return inputError{msg: fmt.Sprintf(format, args...)}
}

func toolFailure(meta toolMeta, stage string, err error) string {
	payload := toolErrorPayload{
		Tool:      meta.Name,
		Stage:     stage,
		Retryable: true,
		Message:   err.Error(),
	}

	switch {
	case errors.Is(err, errInvalidInput):
		payload.ErrorType = "invalid_input"
		payload.Retryable = false
		payload.SuggestedNextActions = []string{
			"fix tool arguments",
			"retry with required fields",
		}
	case errors.Is(err, context.DeadlineExceeded):
		payload.ErrorType = "timeout"
		payload.TimeoutMS = meta.Timeout.Milliseconds()
		payload.SuggestedNextActions = []string{
			"reduce scope",
			"lower top_k",
			"narrow query/path",
			"retry once",
		}
	case errors.Is(err, context.Canceled):
		payload.ErrorType = "cancelled"
		payload.SuggestedNextActions = []string{
			"retry if cancellation was accidental",
		}
	default:
		payload.ErrorType = "internal_error"
		payload.SuggestedNextActions = []string{
			"retry once",
			"check upstream dependency (db/ollama)",
			"fallback to narrower request",
		}
	}

	b, jerr := json.Marshal(payload)
	if jerr != nil {
		return fmt.Sprintf("tool error (%s/%s): %v", meta.Name, stage, err)
	}
	return string(b)
}

func toolLog(meta toolMeta, stage, status string) {
	elapsed := time.Since(meta.Started).Milliseconds()
	fmt.Fprintf(os.Stderr, "tool=%s stage=%s status=%s elapsed_ms=%d\n", meta.Name, stage, status, elapsed)
}
