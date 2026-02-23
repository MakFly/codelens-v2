package mcp

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/yourusername/codelens/internal/indexer"
	"github.com/yourusername/codelens/internal/jit"
	"github.com/yourusername/codelens/internal/store"
)

var allowedMemoryTypes = map[string]struct{}{
	"insight":    {},
	"decision":   {},
	"convention": {},
	"pitfall":    {},
	"runbook":    {},
}

// --- search_codebase ---

func (s *Server) handleSearchCodebase(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctxTool, cancel, meta := withToolTimeout(ctx, "search_codebase")
	defer cancel()
	stage := "input"

	query, err := argString(req, "query", true)
	if err != nil {
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}

	topK := 5
	if v, ok, err := argFloat(req, "top_k"); err != nil {
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	} else if ok && v > 0 {
		topK = int(v)
		if topK > 20 {
			topK = 20
		}
	}

	language, err := argString(req, "language", false)
	if err != nil {
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}
	symbolKind, err := argString(req, "symbol_kind", false)
	if err != nil {
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}

	stage = "search"
	start := time.Now()
	results, err := s.idx.Search(ctxTool, query, topK)
	if err != nil {
		toolLog(meta, stage, "failed")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}

	var filtered []indexer.SearchResult
	for _, r := range results {
		if language != "" && r.Language != language {
			continue
		}
		if symbolKind != "" && r.SymbolKind != symbolKind {
			continue
		}
		filtered = append(filtered, r)
	}

	if len(filtered) == 0 {
		toolLog(meta, stage, "ok")
		body := fmt.Sprintf(
			"No results found for query: %q\n(filters: language=%q, symbol_kind=%q)\n\nTip: Try a broader query or remove filters.",
			query, language, symbolKind,
		)
		return mcp.NewToolResultText(s.withAutoMemoryContext(ctxTool, body, query, 3)), nil
	}

	var sb strings.Builder
	tokenEstimate := 0
	sb.WriteString(fmt.Sprintf("Found %d results for %q (%.0fms)\n\n", len(filtered), query, float64(time.Since(start).Milliseconds())))

	for i, r := range filtered {
		sb.WriteString(fmt.Sprintf("## [%d] %s (lines %d-%d) — score: %.3f\n",
			i+1, r.FilePath, r.StartLine, r.EndLine, r.Score))
		if r.Symbol != "" {
			sb.WriteString(fmt.Sprintf("Symbol: `%s` (%s) · Language: %s\n\n", r.Symbol, r.SymbolKind, r.Language))
		}
		sb.WriteString("```" + r.Language + "\n")
		sb.WriteString(truncateSnippet(r.Content, searchSnippetChars()))
		sb.WriteString("\n```\n\n")
		tokenEstimate += len(truncateSnippet(r.Content, searchSnippetChars())) / 4
	}

	sb.WriteString(fmt.Sprintf("---\n~%d tokens · %d files indexed", tokenEstimate, s.idx.IndexedFileCount()))
	toolLog(meta, stage, "ok")
	return mcp.NewToolResultText(s.withAutoMemoryContext(ctxTool, sb.String(), query, 3)), nil
}

// --- read_file_smart ---

func (s *Server) handleReadFileSmart(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctxTool, cancel, meta := withToolTimeout(ctx, "read_file_smart")
	defer cancel()
	stage := "input"

	path, err := argString(req, "path", true)
	if err != nil {
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}
	query, err := argString(req, "query", false)
	if err != nil {
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}

	stage = "filesystem"
	fullPath := s.idx.ResolvePath(path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		toolLog(meta, stage, "failed")
		return mcp.NewToolResultError(toolFailure(meta, stage, fmt.Errorf("cannot read file %s: %w", path, err))), nil
	}

	lines := strings.Split(string(content), "\n")
	lineCount := len(lines)

	const smartThreshold = 200
	if lineCount <= smartThreshold {
		toolLog(meta, stage, "ok")
		body := fmt.Sprintf("File: %s (%d lines)\n\n```\n%s\n```", path, lineCount, string(content))
		return mcp.NewToolResultText(s.withAutoMemoryContext(ctxTool, body, query+" "+path, 3)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("File: %s (%d lines) — showing relevant sections only\n\n", path, lineCount))
	sb.WriteString("### Structural Outline\n")

	stage = "search"
	results, err := s.idx.SearchInFile(ctxTool, path, query, 10)
	if err != nil || len(results) == 0 {
		sb.WriteString("(index not available for this file — showing head/tail)\n\n")
		head := strings.Join(lines[:min(100, lineCount)], "\n")
		sb.WriteString("```\n" + head + "\n...\n```\n")
		toolLog(meta, stage, "ok")
		return mcp.NewToolResultText(s.withAutoMemoryContext(ctxTool, sb.String(), query+" "+path, 3)), nil
	}

	sb.WriteString(fmt.Sprintf("Found %d relevant sections:\n\n", len(results)))
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("#### `%s` (lines %d-%d)\n", r.Symbol, r.StartLine, r.EndLine))
		sb.WriteString("```" + r.Language + "\n" + truncateSnippet(r.Content, searchSnippetChars()) + "\n```\n\n")
	}

	toolLog(meta, stage, "ok")
	return mcp.NewToolResultText(s.withAutoMemoryContext(ctxTool, sb.String(), query+" "+path, 3)), nil
}

// --- remember / proposals ---

func (s *Server) handleRemember(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Backward compatible alias: remember() now creates a pending proposal.
	return s.handleProposeMemory(ctx, req)
}

func (s *Server) handleProposeMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctxTool, cancel, meta := withToolTimeout(ctx, "propose_memory")
	defer cancel()
	stage := "input"

	insight, err := argString(req, "insight", true)
	if err != nil {
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}
	memoryType, err := parseMemoryType(req)
	if err != nil {
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}
	if _, ok := allowedMemoryTypes[memoryType]; !ok {
		err = invalidInputf("invalid memory_type %q", memoryType)
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}

	stage = "verification"
	citations, err := s.parseAndHashCitations(ctxTool, req)
	if err != nil {
		toolLog(meta, stage, "failed")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}

	stage = "db"
	proposalID, err := s.db.SaveMemoryProposal(ctxTool, insight, memoryType, citations)
	if err != nil {
		toolLog(meta, stage, "failed")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}

	if autoPublishMemoryEnabled() {
		memoryID, pubErr := s.publishProposal(ctxTool, proposalID)
		if pubErr == nil {
			toolLog(meta, stage, "ok")
			return mcp.NewToolResultText(fmt.Sprintf(
				"✓ Memory proposed and published immediately\nProposal: %s\nMemory: %s\nType: %s",
				proposalID, memoryID, memoryType,
			)), nil
		}
		toolLog(meta, "verification", "failed")
		return mcp.NewToolResultError(toolFailure(meta, "verification", pubErr)), nil
	}

	toolLog(meta, stage, "ok")
	return mcp.NewToolResultText(fmt.Sprintf(
		"✓ Memory proposal stored (id: %s)\nType: %s\nCitations: %d\nStatus: pending\n\nRun publish_memory with this proposal_id to activate it.",
		proposalID, memoryType, len(citations),
	)), nil
}

func (s *Server) handlePublishMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctxTool, cancel, meta := withToolTimeout(ctx, "publish_memory")
	defer cancel()
	stage := "input"

	proposalID, err := argString(req, "proposal_id", true)
	if err != nil {
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}

	stage = "db"
	memoryID, err := s.publishProposal(ctxTool, proposalID)
	if err != nil {
		toolLog(meta, "verification", "failed")
		return mcp.NewToolResultError(toolFailure(meta, "verification", err)), nil
	}

	toolLog(meta, stage, "ok")
	return mcp.NewToolResultText(fmt.Sprintf(
		"✓ Memory published\nProposal: %s\nMemory: %s",
		proposalID, memoryID,
	)), nil
}

func (s *Server) parseAndHashCitations(ctx context.Context, req mcp.CallToolRequest) ([]jit.Citation, error) {
	rawCitations, err := argArray(req, "citations", true)
	if err != nil {
		return nil, err
	}

	var citations []jit.Citation
	for _, raw := range rawCitations {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		m, err := argObject(raw, "citations")
		if err != nil {
			return nil, err
		}

		filePath, err := requiredStringField(m, "file")
		if err != nil {
			return nil, err
		}
		lineStart, err := toIntField(m, "line_start")
		if err != nil {
			return nil, err
		}
		lineEnd, err := toIntField(m, "line_end")
		if err != nil {
			return nil, err
		}

		c := jit.Citation{FilePath: filePath, LineStart: lineStart, LineEnd: lineEnd}
		if c.LineStart <= 0 || c.LineEnd < c.LineStart {
			return nil, invalidInputf("invalid citation range for %s:%d-%d", c.FilePath, c.LineStart, c.LineEnd)
		}

		if err := s.verifier.HashCitation(&c); err != nil {
			return nil, fmt.Errorf("cannot hash citation %s:%d-%d: %w", c.FilePath, c.LineStart, c.LineEnd, err)
		}
		citations = append(citations, c)
	}

	if len(citations) == 0 {
		return nil, invalidInputf("at least one valid citation is required")
	}
	return citations, nil
}

func parseMemoryType(req mcp.CallToolRequest) (string, error) {
	v, err := argString(req, "memory_type", false)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "insight", nil
	}
	return strings.TrimSpace(strings.ToLower(v)), nil
}

// --- recall ---

func (s *Server) handleRecall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctxTool, cancel, meta := withToolTimeout(ctx, "recall")
	defer cancel()
	stage := "input"

	query, err := argString(req, "context", true)
	if err != nil {
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}
	limit := 10
	if v, ok, err := argFloat(req, "limit"); err != nil {
		toolLog(meta, stage, "invalid_input")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	} else if ok && v > 0 {
		limit = int(v)
	}

	stage = "db"
	memories, err := s.db.LoadActiveMemories(ctxTool)
	if err != nil {
		toolLog(meta, stage, "failed")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}

	if len(memories) == 0 {
		toolLog(meta, stage, "ok")
		return mcp.NewToolResultText("No memories stored yet for this project. Use `propose_memory()` then `publish_memory()`."), nil
	}

	stage = "verification"
	var valid []store.MemoryRecord
	expired := 0
	for _, mem := range memories {
		select {
		case <-ctxTool.Done():
			toolLog(meta, stage, "failed")
			return mcp.NewToolResultError(toolFailure(meta, stage, ctxTool.Err())), nil
		default:
		}
		if s.verifier.VerifyAll(mem.Citations) {
			valid = append(valid, mem)
		} else {
			expired++
		}
		if len(valid) >= limit {
			break
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Recalling memories relevant to: %q\n", query))
	sb.WriteString(fmt.Sprintf("Found %d valid memories (%d expired/stale discarded)\n\n", len(valid), expired))

	stage = "db"
	for i, mem := range valid {
		sb.WriteString(fmt.Sprintf("### Memory %d · type=%s · hits=%d\n", i+1, mem.Type, mem.HitCount))
		sb.WriteString(mem.Insight + "\n")
		sb.WriteString("Citations:\n")
		for _, c := range mem.Citations {
			sb.WriteString(fmt.Sprintf("  - %s\n", jit.FormatCitationRef(c)))
		}
		sb.WriteString("\n")
		_ = s.db.RecordMemoryHit(ctxTool, mem.ID)
	}

	toolLog(meta, stage, "ok")
	return mcp.NewToolResultText(sb.String()), nil
}

// --- index_status ---

func (s *Server) handleIndexStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctxTool, cancel, meta := withToolTimeout(ctx, "index_status")
	defer cancel()
	stage := "db"

	stats, err := s.db.StatsWithContext(ctxTool)
	if err != nil {
		toolLog(meta, stage, "failed")
		return mcp.NewToolResultError(toolFailure(meta, stage, err)), nil
	}

	result := fmt.Sprintf(`CodeLens Index Status
=====================
Project:         %s
Files indexed:   %d
Chunks:          %d
Failed files:    %d
Active memories: %d
Last indexed:    %s
`,
		s.idx.ProjectRoot(),
		stats.Files,
		stats.Chunks,
		stats.FailedFiles,
		stats.ActiveMemories,
		stats.LastIndexed.Format("2006-01-02 15:04:05"),
	)

	toolLog(meta, stage, "ok")
	return mcp.NewToolResultText(result), nil
}

// --- Resource handler ---

func (s *Server) handleResourceStats(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	ctxTool, cancel, meta := withToolTimeout(ctx, "resource_stats")
	defer cancel()
	stage := "db"

	stats, err := s.db.StatsWithContext(ctxTool)
	if err != nil {
		toolLog(meta, stage, "failed")
		return nil, fmt.Errorf(toolFailure(meta, stage, err))
	}

	json := fmt.Sprintf(`{"files":%d,"chunks":%d,"failed_files":%d,"active_memories":%d,"last_indexed":"%s"}`,
		stats.Files, stats.Chunks, stats.FailedFiles, stats.ActiveMemories,
		stats.LastIndexed.Format(time.RFC3339),
	)
	toolLog(meta, stage, "ok")

	return []mcp.ResourceContents{
		mcp.TextResourceContents{URI: "memory://stats", MIMEType: "application/json", Text: json},
	}, nil
}

func detectContradiction(candidate string, memories []store.MemoryRecord) (string, string) {
	clean := normalizeSentence(candidate)
	for _, mem := range memories {
		existing := normalizeSentence(mem.Insight)
		if sharedTokenRatio(clean, existing) < 0.45 {
			continue
		}
		if hasOppositePolarity(clean, existing) {
			return mem.ID, "similar topic with opposite policy wording"
		}
	}
	return "", ""
}

func normalizeSentence(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	re := regexp.MustCompile(`[^a-z0-9\s]+`)
	s = re.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

func sharedTokenRatio(a, b string) float64 {
	at := strings.Fields(a)
	bt := strings.Fields(b)
	if len(at) == 0 || len(bt) == 0 {
		return 0
	}

	setA := make(map[string]struct{}, len(at))
	for _, t := range at {
		setA[t] = struct{}{}
	}
	shared := 0
	for _, t := range bt {
		if _, ok := setA[t]; ok {
			shared++
		}
	}
	den := len(at)
	if len(bt) < den {
		den = len(bt)
	}
	return float64(shared) / float64(den)
}

func hasOppositePolarity(a, b string) bool {
	pairs := [][2]string{
		{" always ", " never "},
		{" must ", " must not "},
		{" required ", " optional "},
		{" allow ", " deny "},
		{" enabled ", " disabled "},
	}
	paddedA := " " + a + " "
	paddedB := " " + b + " "
	for _, p := range pairs {
		if (strings.Contains(paddedA, p[0]) && strings.Contains(paddedB, p[1])) ||
			(strings.Contains(paddedA, p[1]) && strings.Contains(paddedB, p[0])) {
			return true
		}
	}
	return false
}

func (s *Server) publishProposal(ctx context.Context, proposalID string) (string, error) {
	proposal, err := s.db.GetMemoryProposal(ctx, proposalID)
	if err != nil {
		return "", err
	}
	if proposal.Status != "pending" {
		return "", invalidInputf("proposal %s is %s, only pending proposals can be published", proposalID, proposal.Status)
	}
	if !s.verifier.VerifyAll(proposal.Citations) {
		reason := "citation verification failed: cited lines no longer match current code"
		_ = s.db.RejectMemoryProposal(ctx, proposalID, reason)
		return "", fmt.Errorf(reason)
	}

	memories, err := s.db.LoadActiveMemories(ctx)
	if err != nil {
		return "", err
	}
	if conflictID, reason := detectContradiction(proposal.Insight, memories); conflictID != "" {
		if err := s.db.ArchiveMemory(ctx, conflictID); err != nil {
			return "", err
		}
		_ = reason
	}
	for _, mem := range memories {
		if sameTopic(proposal.Insight, mem.Insight) || hasCitationOverlap(proposal.Citations, mem.Citations) {
			if err := s.db.ArchiveMemory(ctx, mem.ID); err != nil {
				return "", err
			}
		}
	}
	return s.db.PublishMemoryProposal(ctx, proposalID)
}

func sameTopic(a, b string) bool {
	cleanA := normalizeSentence(a)
	cleanB := normalizeSentence(b)
	if cleanA == "" || cleanB == "" {
		return false
	}
	return sharedTokenRatio(cleanA, cleanB) >= 0.55
}

func hasCitationOverlap(a, b []jit.Citation) bool {
	for _, ca := range a {
		for _, cb := range b {
			if ca.FilePath != cb.FilePath {
				continue
			}
			if ca.LineStart <= cb.LineEnd && cb.LineStart <= ca.LineEnd {
				return true
			}
		}
	}
	return false
}

func autoPublishMemoryEnabled() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("CODELENS_MEMORY_AUTO_PUBLISH")))
	if raw == "" {
		return true
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func searchSnippetChars() int {
	raw := strings.TrimSpace(os.Getenv("CODELENS_SNIPPET_CHARS"))
	if raw == "" {
		return 700
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 120 {
		return 700
	}
	if n > 4000 {
		return 4000
	}
	return n
}

func truncateSnippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n// ... truncated by CodeLens for token efficiency ..."
}

func (s *Server) withAutoMemoryContext(ctx context.Context, body, focus string, limit int) string {
	section := s.autoMemorySection(ctx, focus, limit)
	if section == "" {
		return body
	}
	return section + "\n\n" + body
}

func (s *Server) autoMemorySection(ctx context.Context, focus string, limit int) string {
	if limit <= 0 {
		limit = 3
	}

	memories, err := s.db.LoadActiveMemories(ctx)
	if err != nil || len(memories) == 0 {
		return ""
	}

	type scoredMemory struct {
		mem   store.MemoryRecord
		score float64
	}
	focusNorm := normalizeSentence(focus)
	scored := make([]scoredMemory, 0, len(memories))
	for _, mem := range memories {
		if !s.verifier.VerifyAll(mem.Citations) {
			continue
		}
		score := float64(mem.HitCount) / 1000.0
		if focusNorm != "" {
			score += sharedTokenRatio(focusNorm, normalizeSentence(mem.Insight))
		}
		scored = append(scored, scoredMemory{mem: mem, score: score})
	}
	if len(scored) == 0 {
		return ""
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].mem.HitCount > scored[j].mem.HitCount
		}
		return scored[i].score > scored[j].score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}

	var sb strings.Builder
	sb.WriteString("### Auto Memory Context\n")
	for i, item := range scored {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, item.mem.Type, item.mem.Insight))
		maxC := min(2, len(item.mem.Citations))
		for c := 0; c < maxC; c++ {
			sb.WriteString(fmt.Sprintf("   - %s\n", jit.FormatCitationRef(item.mem.Citations[c])))
		}
	}
	return strings.TrimSpace(sb.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
