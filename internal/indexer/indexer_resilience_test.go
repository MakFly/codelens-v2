package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yourusername/codelens/internal/embeddings"
	"github.com/yourusername/codelens/internal/store"
)

type lengthLimitedEmbedder struct {
	maxLines int
	failOn   string
}

func (e *lengthLimitedEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if e.failOn != "" && strings.Contains(text, e.failOn) {
		return nil, errors.New("forced embedding failure")
	}
	if strings.Count(text, "\n")+1 > e.maxLines {
		return nil, embeddings.ErrContextLengthExceeded
	}
	return []float32{1, 0, 0}, nil
}

func (e *lengthLimitedEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		vec, err := e.Embed(ctx, texts[i])
		if err != nil {
			return nil, err
		}
		out[i] = vec
	}
	return out, nil
}

func (e *lengthLimitedEmbedder) Dimensions() int   { return 3 }
func (e *lengthLimitedEmbedder) ModelName() string { return "test" }

func TestIndexAll_SplitsChunksOnContextOverflow(t *testing.T) {
	t.Setenv("CODELENS_SKIP_LOCK_CHECK", "1")
	tmp := t.TempDir()
	db, err := store.Open(filepath.Join(tmp, ".codelens", "index.db"))
	require.NoError(t, err)
	defer db.Close()

	var lines []string
	for i := 0; i < 120; i++ {
		lines = append(lines, "line content")
	}
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "big.php"), []byte(strings.Join(lines, "\n")), 0644))

	idx, err := New(tmp, db, &lengthLimitedEmbedder{maxLines: 20})
	require.NoError(t, err)

	stats, err := idx.IndexAll(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Files)
	require.Equal(t, 0, stats.FailedFiles)
	require.Greater(t, stats.Chunks, 1)
}

func TestIndexAll_RecordsAndSkipsFailures(t *testing.T) {
	t.Setenv("CODELENS_SKIP_LOCK_CHECK", "1")
	tmp := t.TempDir()
	db, err := store.Open(filepath.Join(tmp, ".codelens", "index.db"))
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "ok.go"), []byte("package main\nfunc ok() {}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "bad.go"), []byte("package main\nfunc bad(){\n// FAILME\n}\n"), 0644))

	idx, err := New(tmp, db, &lengthLimitedEmbedder{maxLines: 100, failOn: "FAILME"})
	require.NoError(t, err)

	stats, err := idx.IndexAll(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Files)
	require.Equal(t, 1, stats.FailedFiles)

	dbStats, err := db.Stats()
	require.NoError(t, err)
	require.Equal(t, 1, dbStats.FailedFiles)
}
