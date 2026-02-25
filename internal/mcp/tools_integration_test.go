package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	gmc "github.com/mark3labs/mcp-go/mcp"
	"github.com/yourusername/codelens/internal/embeddings"
	"github.com/yourusername/codelens/internal/indexer"
	"github.com/yourusername/codelens/internal/jit"
	"github.com/yourusername/codelens/internal/store"
)

type slowEmbedder struct{}

func (s *slowEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(200 * time.Millisecond):
		return []float32{1, 0, 0}, nil
	}
}

func (s *slowEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, t := range texts {
		v, err := s.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *slowEmbedder) Dimensions() int   { return 3 }
func (s *slowEmbedder) ModelName() string { return "slow" }

func TestSearchCodebase_TimeoutPayload(t *testing.T) {
	t.Setenv("CODELENS_TOOL_TIMEOUT", "30ms")
	tmp := t.TempDir()
	db, err := store.Open(filepath.Join(tmp, ".codelens", "index.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	idx, err := indexer.New(tmp, db, &slowEmbedder{})
	if err != nil {
		t.Fatalf("new indexer: %v", err)
	}
	srv := NewServer(db, idx, &slowEmbedder{})

	req := gmc.CallToolRequest{}
	req.Params.Name = "search_codebase"
	req.Params.Arguments = map[string]interface{}{"query": "auth"}

	res, err := srv.handleSearchCodebase(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected protocol err: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected tool error result")
	}
	if len(res.Content) == 0 {
		t.Fatal("expected error content")
	}
	text, ok := gmc.AsTextContent(res.Content[0])
	if !ok {
		t.Fatalf("expected text content, got %#v", res.Content[0])
	}
	if !strings.Contains(text.Text, "\"error_type\":\"timeout\"") {
		t.Fatalf("expected timeout error payload, got: %s", text.Text)
	}
	if !strings.Contains(text.Text, "\"tool\":\"search_codebase\"") {
		t.Fatalf("expected tool name in payload, got: %s", text.Text)
	}
}

func TestMemoryFlow_ProposePublishRecall(t *testing.T) {
	t.Setenv("CODELENS_MEMORY_AUTO_PUBLISH", "1")
	tmp := t.TempDir()
	file := filepath.Join(tmp, "sample.php")
	content := "<?php\nfunction important() {\n  return 42;\n}\n"
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	db, err := store.Open(filepath.Join(tmp, ".codelens", "index.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	embedder := embeddings.NewMock(64)
	idx, err := indexer.New(tmp, db, embedder)
	if err != nil {
		t.Fatalf("new indexer: %v", err)
	}
	srv := NewServer(db, idx, embedder)

	proposeReq := gmc.CallToolRequest{}
	proposeReq.Params.Name = "propose_memory"
	proposeReq.Params.Arguments = map[string]interface{}{
		"insight":     "important() centralise le calcul principal",
		"memory_type": "insight",
		"citations": []interface{}{
			map[string]interface{}{
				"file":       "sample.php",
				"line_start": float64(2),
				"line_end":   float64(3),
			},
		},
	}

	proposeRes, err := srv.handleProposeMemory(context.Background(), proposeReq)
	if err != nil {
		t.Fatalf("propose protocol err: %v", err)
	}
	if proposeRes.IsError {
		text, _ := gmc.AsTextContent(proposeRes.Content[0])
		t.Fatalf("unexpected propose tool error: %s", text.Text)
	}
	proposeText, _ := gmc.AsTextContent(proposeRes.Content[0])
	re := regexp.MustCompile(`proposal_[a-z0-9]+`)
	m := re.FindStringSubmatch(proposeText.Text)
	if len(m) < 1 {
		t.Fatalf("could not extract proposal id from: %s", proposeText.Text)
	}
	proposalID := m[0]

	_ = proposalID // auto-publish path covers persistence immediately

	recallReq := gmc.CallToolRequest{}
	recallReq.Params.Name = "recall"
	recallReq.Params.Arguments = map[string]interface{}{"context": "calcul principal", "limit": float64(5)}
	recallRes, err := srv.handleRecall(context.Background(), recallReq)
	if err != nil {
		t.Fatalf("recall protocol err: %v", err)
	}
	if recallRes.IsError {
		text, _ := gmc.AsTextContent(recallRes.Content[0])
		t.Fatalf("unexpected recall tool error: %s", text.Text)
	}
	recallText, _ := gmc.AsTextContent(recallRes.Content[0])
	if !strings.Contains(recallText.Text, "important() centralise le calcul principal") {
		t.Fatalf("recall did not include published memory: %s", recallText.Text)
	}

	var sanity map[string]interface{}
	_ = json.Unmarshal([]byte(`{"ok":true}`), &sanity)
}

func TestMemoryFlow_AutoOverrideSameTopic(t *testing.T) {
	t.Setenv("CODELENS_MEMORY_AUTO_PUBLISH", "1")
	tmp := t.TempDir()
	file := filepath.Join(tmp, "sample.php")
	content := "<?php\nfunction important() {\n  return 42;\n}\n"
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	db, err := store.Open(filepath.Join(tmp, ".codelens", "index.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	embedder := embeddings.NewMock(64)
	idx, err := indexer.New(tmp, db, embedder)
	if err != nil {
		t.Fatalf("new indexer: %v", err)
	}
	srv := NewServer(db, idx, embedder)

	propose := func(insight string) {
		req := gmc.CallToolRequest{}
		req.Params.Name = "propose_memory"
		req.Params.Arguments = map[string]interface{}{
			"insight":     insight,
			"memory_type": "decision",
			"citations": []interface{}{
				map[string]interface{}{
					"file":       "sample.php",
					"line_start": float64(2),
					"line_end":   float64(3),
				},
			},
		}
		res, err := srv.handleProposeMemory(context.Background(), req)
		if err != nil {
			t.Fatalf("propose protocol err: %v", err)
		}
		if res.IsError {
			text, _ := gmc.AsTextContent(res.Content[0])
			t.Fatalf("unexpected propose tool error: %s", text.Text)
		}
	}

	propose("Décision: important() est la source unique du calcul principal.")
	propose("Décision: important() devient l'unique point d'entrée pour ce calcul.")

	active, err := db.LoadActiveMemories(context.Background())
	if err != nil {
		t.Fatalf("load active memories: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active memory after override, got %d", len(active))
	}
	if !strings.Contains(active[0].Insight, "unique point d'entrée") {
		t.Fatalf("expected newest memory to be active, got: %s", active[0].Insight)
	}
}

func TestResourceStats_ContainsEnrichedFields(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "sample.go")
	content := "package main\nfunc main(){}\n"
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	db, err := store.Open(filepath.Join(tmp, ".codelens", "index.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	embedder := embeddings.NewMock(64)
	idx, err := indexer.New(tmp, db, embedder)
	if err != nil {
		t.Fatalf("new indexer: %v", err)
	}
	if _, err := idx.IndexAll(context.Background(), true); err != nil {
		t.Fatalf("index all: %v", err)
	}
	if err := db.RecordIndexFailure(context.Background(), "broken.go", "parse failure"); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	proposalID, err := db.SaveMemoryProposal(context.Background(), "proposal", "insight", []jit.Citation{
		{FilePath: "sample.go", LineStart: 1, LineEnd: 2, Hash: "h1"},
	})
	if err != nil {
		t.Fatalf("save proposal: %v", err)
	}
	if _, err := db.PublishMemoryProposal(context.Background(), proposalID); err != nil {
		t.Fatalf("publish proposal: %v", err)
	}

	srv := NewServer(db, idx, embedder)
	contents, err := srv.handleResourceStats(context.Background(), gmc.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("resource stats error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected one resource content, got %d", len(contents))
	}

	textContent, ok := contents[0].(gmc.TextResourceContents)
	if !ok {
		t.Fatalf("expected TextResourceContents, got %#v", contents[0])
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(textContent.Text), &payload); err != nil {
		t.Fatalf("invalid json payload: %v", err)
	}

	for _, k := range []string{
		"files", "chunks", "failed_files", "active_memories", "last_indexed",
		"embedded_chunks", "embedding_coverage_pct", "avg_chunks_per_file",
		"top_languages", "memory_proposals", "memories", "recent_failures",
	} {
		if _, ok := payload[k]; !ok {
			t.Fatalf("missing expected key %q in payload: %v", k, payload)
		}
	}
}
