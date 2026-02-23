package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
}

func NewOllama(baseURL, model string) *OllamaClient {
	return &OllamaClient{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		dims: defaultDimensions,
	}
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

type ollamaErrorResponse struct {
	Error string `json:"error"`
}

var ErrContextLengthExceeded = errors.New("embedding context length exceeded")

func IsContextLengthExceeded(err error) bool {
	return errors.Is(err, ErrContextLengthExceeded)
}

func (o *OllamaClient) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(ollamaEmbedRequest{Model: o.model, Prompt: text})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var apiErr ollamaErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if strings.Contains(strings.ToLower(apiErr.Error), "context length") {
			return nil, fmt.Errorf("%w: %s", ErrContextLengthExceeded, apiErr.Error)
		}
		if apiErr.Error != "" {
			return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, apiErr.Error)
		}
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode ollama response: %w", err)
	}

	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}

	// Update dims from actual response
	if o.dims == defaultDimensions && len(result.Embedding) != defaultDimensions {
		o.dims = len(result.Embedding)
	}

	return result.Embedding, nil
}

// EmbedBatch sends requests concurrently with a bounded goroutine pool.
func (o *OllamaClient) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	const maxConcurrent = 4

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
