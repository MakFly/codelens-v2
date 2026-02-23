package embeddings

import (
	"context"
	"crypto/md5"
	"math"
)

// MockEmbedder generates deterministic pseudo-embeddings from text.
// Used in unit tests to avoid requiring a running Ollama instance.
type MockEmbedder struct {
	dims int
}

func NewMock(dims int) *MockEmbedder {
	if dims == 0 {
		dims = 64
	}
	return &MockEmbedder{dims: dims}
}

func (m *MockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	hash := md5.Sum([]byte(text))
	vec := make([]float32, m.dims)
	var norm float64
	for i := range vec {
		// Deterministic pseudo-random from hash bytes
		b := float64(hash[i%16]) - 128.0
		vec[i] = float32(b)
		norm += b * b
	}
	// Normalize to unit vector
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= float32(norm)
		}
	}
	return vec, nil
}

func (m *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		emb, err := m.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		results[i] = emb
	}
	return results, nil
}

func (m *MockEmbedder) Dimensions() int   { return m.dims }
func (m *MockEmbedder) ModelName() string { return "mock" }
