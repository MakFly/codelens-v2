package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yourusername/codelens/internal/jit"
)

func TestMemoryProposalPublishFlow(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "index.db"))
	require.NoError(t, err)
	defer db.Close()

	citations := []jit.Citation{
		{FilePath: "src/Auth.php", LineStart: 10, LineEnd: 20, Hash: "abc123"},
	}

	proposalID, err := db.SaveMemoryProposal(context.Background(), "Auth must always validate csrf token", "decision", citations)
	require.NoError(t, err)
	require.NotEmpty(t, proposalID)

	proposal, err := db.GetMemoryProposal(context.Background(), proposalID)
	require.NoError(t, err)
	require.Equal(t, "pending", proposal.Status)
	require.Equal(t, "decision", proposal.Type)
	require.Len(t, proposal.Citations, 1)

	memoryID, err := db.PublishMemoryProposal(context.Background(), proposalID)
	require.NoError(t, err)
	require.NotEmpty(t, memoryID)

	memories, err := db.LoadActiveMemories(context.Background())
	require.NoError(t, err)
	require.Len(t, memories, 1)
	require.Equal(t, "decision", memories[0].Type)
}

func TestRejectMemoryProposal(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "index.db"))
	require.NoError(t, err)
	defer db.Close()

	proposalID, err := db.SaveMemoryProposal(
		context.Background(),
		"Never call repository directly from controller",
		"convention",
		[]jit.Citation{{FilePath: "src/Controller/AuthController.php", LineStart: 1, LineEnd: 5, Hash: "h1"}},
	)
	require.NoError(t, err)

	err = db.RejectMemoryProposal(context.Background(), proposalID, "conflict with published policy")
	require.NoError(t, err)

	_, err = db.PublishMemoryProposal(context.Background(), proposalID)
	require.Error(t, err)
}

