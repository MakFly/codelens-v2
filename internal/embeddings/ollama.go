package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultDimensions = 768 // nomic-embed-text

type OllamaClient struct {
	baseURL    string
	model      string
	httpClient *http.Client
	dims       int
	numThreads int
	maxConc    int
	autoStart  bool

	startMu          sync.Mutex
	lastStartAttempt time.Time
	pullMu           sync.Mutex
	lastPullAttempt  time.Time
}

func NewOllama(baseURL, model string) *OllamaClient {
	return &OllamaClient{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		dims:      defaultDimensions,
		maxConc:   4,
		autoStart: isAutoStartEnabled(),
	}
}

type ollamaEmbedRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt,omitempty"`
	Input   string         `json:"input,omitempty"`
	Options map[string]any `json:"options,omitempty"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

type ollamaEmbedV2Response struct {
	Embeddings [][]float32 `json:"embeddings"`
}

type ollamaErrorResponse struct {
	Error string `json:"error"`
}

type ollamaHTTPError struct {
	Status int
	Msg    string
}

func (e *ollamaHTTPError) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("ollama returned status %d: %s", e.Status, e.Msg)
	}
	return fmt.Sprintf("ollama returned status %d", e.Status)
}

var ErrContextLengthExceeded = errors.New("embedding context length exceeded")

func IsContextLengthExceeded(err error) bool {
	return errors.Is(err, ErrContextLengthExceeded)
}

func (o *OllamaClient) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbedRequest{Model: o.model, Input: text}
	if o.numThreads > 0 {
		reqBody.Options = map[string]any{"num_thread": o.numThreads}
	}

	vector, err := o.embedV2(ctx, reqBody)
	if err == nil {
		return o.finalizeVector(vector), nil
	}
	if !shouldFallbackToLegacy(err) {
		return nil, err
	}

	legacyReq := reqBody
	legacyReq.Prompt = text
	legacyReq.Input = ""
	vector, err = o.embedLegacy(ctx, legacyReq)
	if err != nil {
		return nil, err
	}
	return o.finalizeVector(vector), nil
}

func (o *OllamaClient) embedV2(ctx context.Context, reqBody ollamaEmbedRequest) ([]float32, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	resp, err := o.doJSON(ctx, "/api/embed", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result ollamaEmbedV2Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode ollama response: %w", err)
	}
	if len(result.Embeddings) == 0 || len(result.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}
	return result.Embeddings[0], nil
}

func (o *OllamaClient) embedLegacy(ctx context.Context, reqBody ollamaEmbedRequest) ([]float32, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	resp, err := o.doJSON(ctx, "/api/embeddings", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode ollama response: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}
	return result.Embedding, nil
}

func (o *OllamaClient) doJSON(ctx context.Context, path string, body []byte) (*http.Response, error) {
	// One retry is intentional for self-healing:
	// - First failure may happen when local Ollama daemon is down
	// - First 404 may happen when the embedding model is not pulled yet
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := o.httpClient.Do(req)
		if err != nil {
			if attempt == 0 && o.autoStart && isConnectionRefused(err) {
				if recoverErr := o.recoverConnection(ctx); recoverErr == nil {
					continue
				}
			}
			return nil, fmt.Errorf("ollama request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			var apiErr ollamaErrorResponse
			_ = json.Unmarshal(raw, &apiErr)
			_ = resp.Body.Close()
			lowerAPIErr := strings.ToLower(apiErr.Error)
			lowerRaw := strings.ToLower(strings.TrimSpace(string(raw)))
			if strings.Contains(lowerAPIErr, "context length") || strings.Contains(lowerRaw, "context length") {
				return nil, fmt.Errorf("%w: %s", ErrContextLengthExceeded, apiErr.Error)
			}
			if attempt == 0 && o.autoStart && isModelMissing(lowerAPIErr, lowerRaw) {
				if pullErr := o.pullModel(ctx); pullErr == nil {
					continue
				}
			}
			if apiErr.Error != "" {
				return nil, &ollamaHTTPError{Status: resp.StatusCode, Msg: apiErr.Error}
			}
			msg := strings.TrimSpace(string(raw))
			return nil, &ollamaHTTPError{Status: resp.StatusCode, Msg: msg}
		}
		return resp, nil
	}

	return nil, fmt.Errorf("ollama request failed after retry")
}

func shouldFallbackToLegacy(err error) bool {
	if IsContextLengthExceeded(err) {
		return false
	}
	httpErr := &ollamaHTTPError{}
	if errors.As(err, &httpErr) {
		if httpErr.Status == http.StatusNotFound || httpErr.Status == http.StatusMethodNotAllowed || httpErr.Status == http.StatusNotImplemented {
			return true
		}
		lower := strings.ToLower(httpErr.Msg)
		return strings.Contains(lower, "not found") || strings.Contains(lower, "unknown route")
	}
	return false
}

func (o *OllamaClient) finalizeVector(vector []float32) []float32 {
	if o.dims == defaultDimensions && len(vector) != defaultDimensions {
		o.dims = len(vector)
	}
	return vector
}

// EmbedBatch sends requests concurrently with a bounded goroutine pool.
func (o *OllamaClient) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	maxConcurrent := o.maxConc
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	results := make([][]float32, len(texts))
	errCh := make(chan error, 1)
	sem := make(chan struct{}, maxConcurrent)

	for i, text := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case sem <- struct{}{}:
		}

		go func(idx int, t string) {
			defer func() { <-sem }()
			emb, err := o.Embed(ctx, t)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("embed[%d]: %w", idx, err):
				default:
				}
				return
			}
			results[idx] = emb
		}(i, text)
	}

	// Drain semaphore (wait for all goroutines)
	for i := 0; i < maxConcurrent; i++ {
		sem <- struct{}{}
	}

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	return results, nil
}

func (o *OllamaClient) Dimensions() int   { return o.dims }
func (o *OllamaClient) ModelName() string { return o.model }

func (o *OllamaClient) SetTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	o.httpClient.Timeout = timeout
}

func (o *OllamaClient) SetNumThreads(n int) {
	if n < 0 {
		n = 0
	}
	o.numThreads = n
}

func (o *OllamaClient) SetMaxConcurrent(n int) {
	if n < 1 {
		n = 1
	}
	o.maxConc = n
}

func isAutoStartEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("CODELENS_OLLAMA_AUTOSTART")))
	if v == "" {
		return true
	}
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		if strings.Contains(strings.ToLower(netErr.Err.Error()), "connection refused") {
			return true
		}
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "connection refused") || strings.Contains(lower, "connect: cannot assign requested address")
}

func isModelMissing(lowerAPIErr, lowerRaw string) bool {
	return strings.Contains(lowerAPIErr, "model") && strings.Contains(lowerAPIErr, "not found") ||
		strings.Contains(lowerRaw, "model") && strings.Contains(lowerRaw, "not found")
}

func (o *OllamaClient) recoverConnection(ctx context.Context) error {
	if err := o.waitForHealthy(ctx, 500*time.Millisecond); err == nil {
		return nil
	}
	if err := o.startLocalOllama(); err != nil {
		return err
	}
	return o.waitForHealthy(ctx, 3*time.Second)
}

func (o *OllamaClient) startLocalOllama() error {
	o.startMu.Lock()
	defer o.startMu.Unlock()

	now := time.Now()
	if !o.lastStartAttempt.IsZero() && now.Sub(o.lastStartAttempt) < 3*time.Second {
		return fmt.Errorf("ollama startup already attempted recently")
	}
	o.lastStartAttempt = now

	cmd := exec.Command("ollama", "serve")
	cmd.Env = withOllamaDefaults(os.Environ())
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start local ollama serve: %w", err)
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return nil
}

func withOllamaDefaults(env []string) []string {
	current := map[string]string{}
	for _, kv := range env {
		idx := strings.Index(kv, "=")
		if idx <= 0 {
			continue
		}
		current[kv[:idx]] = kv[idx+1:]
	}
	if current["HOME"] == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			env = append(env, "HOME="+home)
			current["HOME"] = home
		}
	}
	if current["OLLAMA_MODELS"] == "" && current["HOME"] != "" {
		env = append(env, "OLLAMA_MODELS="+filepath.Join(current["HOME"], ".ollama", "models"))
	}
	return env
}

func (o *OllamaClient) waitForHealthy(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := o.ping(waitCtx); err == nil {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("ollama is not reachable at %s", o.baseURL)
		case <-ticker.C:
		}
	}
}

func (o *OllamaClient) ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(o.baseURL, "/")+"/api/version", nil)
	if err != nil {
		return err
	}
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

func (o *OllamaClient) pullModel(ctx context.Context) error {
	o.pullMu.Lock()
	defer o.pullMu.Unlock()

	now := time.Now()
	if !o.lastPullAttempt.IsZero() && now.Sub(o.lastPullAttempt) < 20*time.Second {
		return fmt.Errorf("model pull already attempted recently")
	}
	o.lastPullAttempt = now

	pullCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(pullCtx, "ollama", "pull", o.model)
	cmd.Env = withOllamaDefaults(os.Environ())
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return fmt.Errorf("pull model %q: %w", o.model, err)
	}
	return nil
}
