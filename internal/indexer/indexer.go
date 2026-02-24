package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yourusername/codelens/internal/embeddings"
	"github.com/yourusername/codelens/internal/store"
)

type Indexer struct {
	projectRoot string
	db          *store.DB
	embedder    embeddings.Embedder
}

var excludedRootDirs = map[string]struct{}{
	".git":         {},
	".codelens":    {},
	"node_modules": {},
	"vendor":       {},
	"dist":         {},
	"build":        {},
	"coverage":     {},
	".next":        {},
	".nuxt":        {},
	".turbo":       {},
	".rush":        {},
	".yarn":        {},
	".pnpm-store":  {},
	"var":          {},
	"tmp":          {},
	"temp":         {},
	"logs":         {},
	"log":          {},
	"storage":      {},
}

var excludedPathPrefixes = []string{
	"var/cache/",
	"public/bundles/",
	"public/build/",
	"public/assets/",
}

var excludedFileSuffixes = []string{
	".min.js",
	".min.css",
	".map",
}

type IndexStats struct {
	Files       int
	Chunks      int
	FailedFiles int
	Duration    time.Duration
}

type SearchResult struct {
	ChunkID    string
	FilePath   string
	StartLine  int
	EndLine    int
	Content    string
	Language   string
	Symbol     string
	SymbolKind string
	Score      float32
}

func New(projectRoot string, db *store.DB, embedder embeddings.Embedder) (*Indexer, error) {
	if db == nil {
		return nil, errors.New("db is required")
	}
	if embedder == nil {
		return nil, errors.New("embedder is required")
	}
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	return &Indexer{
		projectRoot: abs,
		db:          db,
		embedder:    embedder,
	}, nil
}

func (i *Indexer) ProjectRoot() string { return i.projectRoot }

func (i *Indexer) ResolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(i.projectRoot, path)
}

func (i *Indexer) IndexAll(ctx context.Context, force bool) (*IndexStats, error) {
	start := time.Now()
	stats := &IndexStats{}
	if err := i.purgeExcludedArtifacts(ctx); err != nil {
		return nil, err
	}

	err := filepath.WalkDir(i.projectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(i.projectRoot, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if shouldSkipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldSkipFile(rel) {
			// If a file is now excluded, clear old failure records from previous indexing runs.
			if err := i.db.ClearIndexFailuresByFile(ctx, rel); err != nil {
				return err
			}
			return nil
		}

		lang := languageFromPath(path)
		if lang == "" {
			return nil
		}

		contentBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if isFileBeingWritten(path) {
			return nil
		}
		content := string(contentBytes)
		fileHash := hashString(content)

		if !force {
			prevHash, err := i.db.GetFileHash(ctx, rel)
			if err == nil && prevHash == fileHash {
				return nil
			}
		}

		embedded, err := i.buildEmbeddedChunks(ctx, rel, content, lang)
		if err != nil {
			stats.FailedFiles++
			if recErr := i.db.RecordIndexFailure(ctx, rel, err.Error()); recErr != nil {
				return fmt.Errorf("index file %s: %w (record failure: %v)", rel, err, recErr)
			}
			return nil
		}

		if err := i.db.DeleteChunksByFile(ctx, rel); err != nil {
			return err
		}

		for _, e := range embedded {
			if err := i.db.UpsertChunk(ctx, toStoreChunk(e.chunk), e.embedding); err != nil {
				stats.FailedFiles++
				if recErr := i.db.RecordIndexFailure(ctx, rel, err.Error()); recErr != nil {
					return fmt.Errorf("upsert chunk for %s: %w (record failure: %v)", rel, err, recErr)
				}
				return nil
			}
		}

		if err := i.db.SetFileHash(ctx, rel, fileHash); err != nil {
			return err
		}
		if err := i.db.ClearIndexFailuresByFile(ctx, rel); err != nil {
			return err
		}

		stats.Files++
		stats.Chunks += len(embedded)
		return nil
	})
	if err != nil {
		return nil, err
	}

	stats.Duration = time.Since(start)
	return stats, nil
}

type embeddedChunk struct {
	chunk     Chunk
	embedding []float32
}

func (i *Indexer) buildEmbeddedChunks(ctx context.Context, rel, content, lang string) ([]embeddedChunk, error) {
	chunks := ChunkFile(rel, content, lang)
	if len(chunks) == 0 {
		return []embeddedChunk{}, nil
	}

	out := make([]embeddedChunk, 0, len(chunks))
	for _, chunk := range chunks {
		chunk.Hash = hashString(chunk.Content)
		embeds, err := i.embedChunkWithFallback(ctx, chunk)
		if err != nil {
			return nil, err
		}
		out = append(out, embeds...)
	}
	return out, nil
}

func (i *Indexer) embedChunkWithFallback(ctx context.Context, chunk Chunk) ([]embeddedChunk, error) {
	vector, err := i.embedder.Embed(ctx, chunk.Content)
	if err == nil {
		return []embeddedChunk{{chunk: chunk, embedding: vector}}, nil
	}
	if !embeddings.IsContextLengthExceeded(err) {
		return nil, err
	}

	lines := strings.Split(chunk.Content, "\n")
	if len(lines) <= ChunkMinLines {
		return nil, err
	}

	mid := len(lines) / 2
	left := strings.Join(lines[:mid], "\n")
	right := strings.Join(lines[mid:], "\n")

	leftChunk := chunk
	leftChunk.EndLine = chunk.StartLine + mid - 1
	leftChunk.Content = left
	leftChunk.ID = NewChunkID(leftChunk.FilePath, leftChunk.StartLine)
	leftChunk.Hash = hashString(leftChunk.Content)

	rightChunk := chunk
	rightChunk.StartLine = leftChunk.EndLine + 1
	rightChunk.Content = right
	rightChunk.ID = NewChunkID(rightChunk.FilePath, rightChunk.StartLine)
	rightChunk.Hash = hashString(rightChunk.Content)

	leftEmbeds, err := i.embedChunkWithFallback(ctx, leftChunk)
	if err != nil {
		return nil, err
	}
	rightEmbeds, err := i.embedChunkWithFallback(ctx, rightChunk)
	if err != nil {
		return nil, err
	}
	return append(leftEmbeds, rightEmbeds...), nil
}

func (i *Indexer) Watch(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	var watchMu sync.Mutex
	watchedDirs := make(map[string]bool)

	addWatchRecursive := func(path string) error {
		return filepath.WalkDir(path, func(wp string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				rel, relErr := filepath.Rel(i.projectRoot, wp)
				if relErr != nil {
					return relErr
				}
				if shouldSkipDir(rel) {
					return filepath.SkipDir
				}
				watchMu.Lock()
				if !watchedDirs[wp] {
					watchedDirs[wp] = true
					watchMu.Unlock()
					if addErr := watcher.Add(wp); addErr != nil {
						return addErr
					}
				} else {
					watchMu.Unlock()
				}
			}
			return nil
		})
	}

	if err := addWatchRecursive(i.projectRoot); err != nil {
		return fmt.Errorf("add watch: %w", err)
	}

	var cycleMu sync.Mutex
	var cycleRunning bool

	runCycle := func() {
		cycleMu.Lock()
		if cycleRunning {
			cycleMu.Unlock()
			return
		}
		cycleRunning = true
		cycleMu.Unlock()

		defer func() {
			cycleMu.Lock()
			cycleRunning = false
			cycleMu.Unlock()
		}()

		if _, err := i.IndexAll(ctx, false); err != nil {
			fmt.Fprintf(os.Stderr, "index cycle error: %v\n", err)
		}
	}

	debounce := time.NewTimer(500 * time.Millisecond)
	debounce.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-watcher.Events:
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				if !debounce.Stop() {
					select {
					case <-debounce.C:
					default:
					}
				}
				debounce.Reset(500 * time.Millisecond)
			}
		case <-debounce.C:
			go runCycle()
		case err := <-watcher.Errors:
			if err != nil {
				return fmt.Errorf("watcher error: %w", err)
			}
		}
	}
}

func (i *Indexer) Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	if topK <= 0 {
		topK = 5
	}

	qv, err := i.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	records, err := i.db.LoadAllEmbeddings(ctx)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return []SearchResult{}, nil
	}

	type scored struct {
		id    string
		score float32
	}
	scoredChunks := make([]scored, 0, len(records))
	for _, r := range records {
		scoredChunks = append(scoredChunks, scored{
			id:    r.ChunkID,
			score: cosine(qv, r.Embedding),
		})
	}

	sort.Slice(scoredChunks, func(a, b int) bool {
		return scoredChunks[a].score > scoredChunks[b].score
	})
	if topK > len(scoredChunks) {
		topK = len(scoredChunks)
	}
	scoredChunks = scoredChunks[:topK]

	ids := make([]string, 0, len(scoredChunks))
	scores := make(map[string]float32, len(scoredChunks))
	for _, s := range scoredChunks {
		ids = append(ids, s.id)
		scores[s.id] = s.score
	}

	chunks, err := i.db.GetChunksByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	chunkByID := make(map[string]store.ChunkRecord, len(chunks))
	for _, c := range chunks {
		chunkByID[c.ID] = c
	}

	results := make([]SearchResult, 0, len(scoredChunks))
	for _, s := range scoredChunks {
		c, ok := chunkByID[s.id]
		if !ok {
			continue
		}
		results = append(results, SearchResult{
			ChunkID:    c.ID,
			FilePath:   c.FilePath,
			StartLine:  c.StartLine,
			EndLine:    c.EndLine,
			Content:    c.Content,
			Language:   c.Language,
			Symbol:     c.Symbol,
			SymbolKind: c.SymbolKind,
			Score:      s.score,
		})
	}

	return results, nil
}

func (i *Indexer) SearchInFile(ctx context.Context, path, query string, topK int) ([]SearchResult, error) {
	rel := path
	if filepath.IsAbs(path) {
		v, err := filepath.Rel(i.projectRoot, path)
		if err == nil {
			rel = v
		}
	}
	rel = filepath.ToSlash(filepath.Clean(rel))

	results, err := i.Search(ctx, query, max(topK*3, 10))
	if err != nil {
		return nil, err
	}

	filtered := make([]SearchResult, 0, topK)
	for _, r := range results {
		if filepath.ToSlash(filepath.Clean(r.FilePath)) != rel {
			continue
		}
		filtered = append(filtered, r)
		if len(filtered) >= topK {
			break
		}
	}
	return filtered, nil
}

func (i *Indexer) IndexedFileCount() int {
	stats, err := i.db.Stats()
	if err != nil {
		return 0
	}
	return stats.Files
}

func languageFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".php":
		return "php"
	case ".ts", ".tsx", ".js", ".jsx":
		return "typescript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	default:
		return ""
	}
}

func toStoreChunk(chunk Chunk) store.ChunkRecord {
	return store.ChunkRecord{
		ID:         chunk.ID,
		FilePath:   chunk.FilePath,
		StartLine:  chunk.StartLine,
		EndLine:    chunk.EndLine,
		Content:    chunk.Content,
		Language:   chunk.Language,
		Symbol:     chunk.Symbol,
		SymbolKind: chunk.SymbolKind,
		Hash:       chunk.Hash,
	}
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func cosine(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float32
	for idx := 0; idx < n; idx++ {
		dot += a[idx] * b[idx]
		na += a[idx] * a[idx]
		nb += b[idx] * b[idx]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (sqrt32(na) * sqrt32(nb))
}

func sqrt32(v float32) float32 {
	return float32(math.Sqrt(float64(v)))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func shouldSkipDir(rel string) bool {
	if rel == "." {
		return false
	}
	root := rel
	if idx := strings.IndexByte(rel, '/'); idx >= 0 {
		root = rel[:idx]
	}
	root = strings.ToLower(root)
	if _, ok := excludedRootDirs[root]; ok {
		return true
	}

	lower := strings.ToLower(rel)
	for _, p := range excludedPathPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func isFileBeingWritten(path string) bool {
	if os.Getenv("CODELENS_SKIP_LOCK_CHECK") == "1" {
		return false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	if time.Since(fi.ModTime()) < 500*time.Millisecond {
		return true
	}
	return false
}

func shouldSkipFile(rel string) bool {
	lower := strings.ToLower(rel)
	for _, p := range excludedPathPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	for _, s := range excludedFileSuffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}

func (i *Indexer) purgeExcludedArtifacts(ctx context.Context) error {
	prefixes := make([]string, 0, len(excludedPathPrefixes)+len(excludedRootDirs))
	prefixes = append(prefixes, excludedPathPrefixes...)
	for root := range excludedRootDirs {
		prefixes = append(prefixes, strings.ToLower(root)+"/")
	}
	if err := i.db.PurgeExcludedByPrefixes(ctx, prefixes); err != nil {
		return err
	}
	if err := i.db.PurgeExcludedBySuffixes(ctx, excludedFileSuffixes); err != nil {
		return err
	}
	return nil
}
