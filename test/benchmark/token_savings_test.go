package benchmark

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/yourusername/codelens/internal/embeddings"
	"github.com/yourusername/codelens/internal/indexer"
	"github.com/yourusername/codelens/internal/store"
)

// TokenSavingsBenchmark compares token consumption between:
//   - Traditional approach: grep + read files
//   - CodeLens approach: semantic_search
//
// Run with: go test ./test/benchmark/... -run TestTokenSavings -v
// Requires: Ollama running with nomic-embed-text, a real project indexed.

const testProjectPath = "../.." // codelens-v2 itself (dogfooding)

var benchmarkQueries = []string{
	"MCP server tool handler",
	"embedding generation client",
	"chunk PHP class methods",
	"JIT citation verifier",
	"SQLite schema creation",
}

func TestTokenSavings_Comparison(t *testing.T) {
	if os.Getenv("CODELENS_BENCHMARK") == "" {
		t.Skip("Set CODELENS_BENCHMARK=1 to run token savings benchmark")
	}

	db, err := store.Open(testProjectPath + "/.codelens/index.db")
	if err != nil {
		t.Fatalf("open db: %v (run 'codelens index .' first)", err)
	}
	defer db.Close()

	embedder := embeddings.NewOllama("http://localhost:11434", "nomic-embed-text")
	idx, err := indexer.New(testProjectPath, db, embedder)
	if err != nil {
		t.Fatalf("create indexer: %v", err)
	}

	type result struct {
		query       string
		grepTokens  int
		lensTokens  int
		ratio       float64
	}

	var results []result

	for _, query := range benchmarkQueries {
		// --- Grep approach ---
		grepTokens := measureGrepTokens(query)

		// --- CodeLens semantic approach ---
		chunks, err := idx.Search(context.Background(), query, 5)
		if err != nil {
			t.Logf("search error for %q: %v", query, err)
			continue
		}

		lensContent := ""
		for _, c := range chunks {
			lensContent += c.Content + "\n"
		}
		lensTokens := estimateTokens(lensContent)

		ratio := 0.0
		if lensTokens > 0 {
			ratio = float64(grepTokens) / float64(lensTokens)
		}

		results = append(results, result{query, grepTokens, lensTokens, ratio})
	}

	// Print comparison table
	fmt.Println("\n=== Token Savings Benchmark ===")
	fmt.Printf("%-45s %10s %10s %8s\n", "Query", "Grep tokens", "Lens tokens", "Ratio")
	fmt.Println(strings.Repeat("-", 80))

	totalGrep, totalLens := 0, 0
	for _, r := range results {
		fmt.Printf("%-45s %10d %10d %7.1fx\n", truncate(r.query, 44), r.grepTokens, r.lensTokens, r.ratio)
		totalGrep += r.grepTokens
		totalLens += r.lensTokens
	}

	fmt.Println(strings.Repeat("-", 80))
	overallRatio := 0.0
	if totalLens > 0 {
		overallRatio = float64(totalGrep) / float64(totalLens)
	}
	fmt.Printf("%-45s %10d %10d %7.1fx\n", "TOTAL", totalGrep, totalLens, overallRatio)

	if overallRatio < 3.0 {
		t.Errorf("Token savings ratio %.1fx is below target of 3x", overallRatio)
	}
}

func BenchmarkSearchLatency_WithIndex(b *testing.B) {
	if os.Getenv("CODELENS_BENCHMARK") == "" {
		b.Skip("Set CODELENS_BENCHMARK=1 to run latency benchmark")
	}

	db, _ := store.Open(testProjectPath + "/.codelens/index.db")
	defer db.Close()

	embedder := embeddings.NewOllama("http://localhost:11434", "nomic-embed-text")
	idx, _ := indexer.New(testProjectPath, db, embedder)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Search(context.Background(), "authentication logic", 5)
	}
}

// measureGrepTokens simulates grep + read approach and counts resulting tokens.
func measureGrepTokens(query string) int {
	// Simulate: grep -r "keyword" . --include="*.go" --include="*.php" --include="*.ts"
	keywords := strings.Fields(query)
	if len(keywords) == 0 {
		return 0
	}

	cmd := exec.Command("grep", "-r", "--include=*.go", "--include=*.php",
		"--include=*.ts", "-l", keywords[0], testProjectPath)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}

	// Simulate reading each matched file
	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	total := 0
	for _, f := range files {
		if f == "" {
			continue
		}
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		total += estimateTokens(string(content))
	}
	return total
}

// estimateTokens roughly estimates token count (1 token ≈ 4 chars, common heuristic).
func estimateTokens(content string) int {
	return len(content) / 4
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
