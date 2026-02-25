package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
}

func NewOllama(baseURL, model string) *OllamaClient {
	return &OllamaClient{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		dims:    defaultDimensions,
		maxConc: 4,
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
	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
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
		if apiErr.Error != "" {
			return nil, &ollamaHTTPError{Status: resp.StatusCode, Msg: apiErr.Error}
		}
		msg := strings.TrimSpace(string(raw))
		return nil, &ollamaHTTPError{Status: resp.StatusCode, Msg: msg}
	}
	return resp, nil
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
