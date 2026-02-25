package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yourusername/codelens/internal/jit"
)

func TestStatsWithContext_EmptyDB(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "index.db"))
	require.NoError(t, err)
	defer db.Close()

	stats, err := db.StatsWithContext(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, stats.Files)
	require.Equal(t, 0, stats.Chunks)
	require.Equal(t, 0, stats.EmbeddedChunks)
	require.Equal(t, 0.0, stats.EmbeddingCoveragePct)
	require.Equal(t, 0.0, stats.AvgChunksPerFile)
	require.Empty(t, stats.TopLanguages)
	require.Equal(t, 0, stats.MemoryProposals.Pending)
	require.Equal(t, 0, stats.MemoryProposals.Published)
	require.Equal(t, 0, stats.MemoryProposals.Rejected)
	require.Equal(t, 0, stats.Memories.Archived)
	require.Equal(t, 0, stats.Memories.ExpiredPublished)
	require.Equal(t, 0, stats.Failures.Last24h)
	require.Equal(t, 0, stats.Failures.Last7d)
	require.True(t, stats.LastIndexed.IsZero())
	require.True(t, stats.Failures.LastFailure.IsZero())
}

func TestStatsWithContext_EnrichedMetrics(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "index.db"))
	require.NoError(t, err)
	defer db.Close()

	// One chunk with embedding (Go)
	err = db.UpsertChunk(ctx, ChunkRecord{
		ID:        "chunk-go-1",
		FilePath:  "main.go",
		StartLine: 1,
		EndLine:   10,
		Content:   "package main",
		Language:  "go",
		Hash:      "h-go-1",
	}, []float32{0.1, 0.2, 0.3})
	require.NoError(t, err)

	// One chunk without embedding (TS) to validate partial coverage.
	_, err = db.conn.ExecContext(ctx, `
		INSERT INTO chunks (id, file_path, start_line, end_line, content, language, hash, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "chunk-ts-1", "web/app.ts", 1, 8, "export const x = 1", "typescript", "h-ts-1", time.Now().Add(-2*time.Hour))
	require.NoError(t, err)

	// Active + archived + expired memories.
	_, err = db.SavePublishedMemory(ctx, "active memory", "insight", []jit.Citation{
		{FilePath: "main.go", LineStart: 1, LineEnd: 5, Hash: "h-go-1"},
	})
	require.NoError(t, err)
	_, err = db.conn.ExecContext(ctx, `
		INSERT INTO memories (id, insight, memory_type, status, published_at, expires_at)
		VALUES (?, ?, ?, 'archived', ?, ?)
	`, "mem_archived", "archived memory", "decision", time.Now().Add(-10*time.Hour), time.Now().Add(24*time.Hour))
	require.NoError(t, err)
	_, err = db.conn.ExecContext(ctx, `
		INSERT INTO memories (id, insight, memory_type, status, published_at, expires_at)
		VALUES (?, ?, ?, 'published', ?, ?)
	`, "mem_expired", "expired memory", "pitfall", time.Now().Add(-10*time.Hour), time.Now().Add(-1*time.Hour))
	require.NoError(t, err)

	// Proposals: pending + rejected + published.
	pendingID, err := db.SaveMemoryProposal(ctx, "pending proposal", "insight", []jit.Citation{
		{FilePath: "main.go", LineStart: 1, LineEnd: 5, Hash: "h-go-1"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, pendingID)
	rejectedID, err := db.SaveMemoryProposal(ctx, "rejected proposal", "convention", []jit.Citation{
		{FilePath: "main.go", LineStart: 1, LineEnd: 5, Hash: "h-go-1"},
	})
	require.NoError(t, err)
	require.NoError(t, db.RejectMemoryProposal(ctx, rejectedID, "not applicable"))
	publishedProposalID, err := db.SaveMemoryProposal(ctx, "published proposal", "runbook", []jit.Citation{
		{FilePath: "main.go", LineStart: 1, LineEnd: 5, Hash: "h-go-1"},
	})
	require.NoError(t, err)
	_, err = db.PublishMemoryProposal(ctx, publishedProposalID)
	require.NoError(t, err)

	// Failures: one recent, one old.
	_, err = db.conn.ExecContext(ctx, `
		INSERT INTO index_failures (file_path, error, created_at) VALUES
		('src/recent.go', 'recent error', ?),
		('src/old.go', 'old error', ?)
	`, time.Now().Add(-2*time.Hour), time.Now().Add(-9*24*time.Hour))
	require.NoError(t, err)

	stats, err := db.StatsWithContext(ctx)
	require.NoError(t, err)

	require.Equal(t, 2, stats.Files)
	require.Equal(t, 2, stats.Chunks)
	require.Equal(t, 1, stats.EmbeddedChunks)
	require.InDelta(t, 50.0, stats.EmbeddingCoveragePct, 0.01)
	require.InDelta(t, 1.0, stats.AvgChunksPerFile, 0.01)
	require.Len(t, stats.TopLanguages, 2)
	require.Equal(t, 1, stats.TopLanguages[0].Chunks)
	require.Equal(t, 1, stats.TopLanguages[1].Chunks)

	require.Equal(t, 2, stats.ActiveMemories)
	require.Equal(t, 1, stats.Memories.Archived)
	require.Equal(t, 1, stats.Memories.ExpiredPublished)

	require.Equal(t, 1, stats.MemoryProposals.Pending)
	require.Equal(t, 1, stats.MemoryProposals.Published)
	require.Equal(t, 1, stats.MemoryProposals.Rejected)

	require.Equal(t, 2, stats.FailedFiles)
	require.Equal(t, 1, stats.Failures.Last24h)
	require.Equal(t, 1, stats.Failures.Last7d)
	require.False(t, stats.Failures.LastFailure.IsZero())
	require.False(t, stats.LastIndexed.IsZero())
}
