package embeddings

import "context"

// Embedder is the interface for generating text embeddings.
// Swap implementations: Ollama (local), OpenAI, mock for tests.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
	ModelName() string
}
