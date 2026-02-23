package indexer

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/cespare/xxhash/v2"
)

const (
	ChunkMinLines     = 10
	ChunkMaxLines     = 150
	ChunkOverlapLines = 3
)

// Chunk is the atomic unit of indexation — a semantically meaningful
// section of code (function, class, method, or generic block).
type Chunk struct {
	ID         string // SHA256(filepath:start_line)
	FilePath   string
	StartLine  int
	EndLine    int
	Content    string
	Language   string
	Symbol     string // e.g., "AuthService::login"
	SymbolKind string // "function", "class", "method", "interface"
	Hash       string // xxhash of Content for change detection
}

// NewChunkID generates a stable ID from file path and start line.
func NewChunkID(filePath string, startLine int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", filePath, startLine)))
	return fmt.Sprintf("%x", h[:8]) // 16-char hex
}

// ChunkFile splits a source file into chunks. It tries AST-aware chunking
// first (if a language grammar is available), then falls back to generic.
func ChunkFile(filePath, content, language string) []Chunk {
	lines := strings.Split(content, "\n")

	var chunks []Chunk

	// Try language-specific chunker first
	switch language {
	case "php":
		chunks = chunkPHP(filePath, lines)
		if len(chunks) > 0 {
			break
		}
	case "typescript", "javascript", "tsx", "jsx":
		chunks = chunkTypeScript(filePath, lines)
		if len(chunks) > 0 {
			break
		}
	case "go":
		chunks = chunkGo(filePath, lines)
		if len(chunks) > 0 {
			break
		}
	}

	// Generic fallback: fixed-size blocks with overlap
	if len(chunks) == 0 {
		chunks = chunkGeneric(filePath, lines, language)
	}
	for idx := range chunks {
		chunks[idx].Hash = fmt.Sprintf("%x", xxhash.Sum64String(chunks[idx].Content))
	}
	return chunks
}

// chunkGeneric splits into overlapping blocks of ChunkMaxLines.
func chunkGeneric(filePath string, lines []string, language string) []Chunk {
	var chunks []Chunk
	total := len(lines)
	step := ChunkMaxLines - ChunkOverlapLines

	for start := 0; start < total; start += step {
		end := start + ChunkMaxLines
		if end > total {
			end = total
		}

		block := lines[start:end]
		content := strings.Join(block, "\n")

		if len(strings.TrimSpace(content)) == 0 {
			continue
		}

		chunk := Chunk{
			ID:        NewChunkID(filePath, start+1),
			FilePath:  filePath,
			StartLine: start + 1, // 1-based
			EndLine:   end,
			Content:   content,
			Language:  language,
		}
		chunks = append(chunks, chunk)

		if end >= total {
			break
		}
	}

	return chunks
}

// chunkBySymbol is the common pattern used by language-specific chunkers.
// boundaries is a list of (startLine 0-based, endLine 0-based, symbol, kind).
func chunkBySymbol(filePath string, lines []string, language string, boundaries []symbolBoundary) []Chunk {
	var chunks []Chunk

	for _, b := range boundaries {
		start := b.start
		end := b.end
		if end >= len(lines) {
			end = len(lines) - 1
		}

		// Split oversized symbols into sub-chunks
		if end-start+1 > ChunkMaxLines {
			sub := chunkGeneric(filePath, lines[start:end+1], language)
			for i := range sub {
				sub[i].StartLine += start
				sub[i].EndLine += start
				sub[i].ID = NewChunkID(filePath, sub[i].StartLine)
				sub[i].Symbol = b.symbol
				sub[i].SymbolKind = b.kind
			}
			chunks = append(chunks, sub...)
			continue
		}

		content := strings.Join(lines[start:end+1], "\n")
		if len(strings.TrimSpace(content)) == 0 {
			continue
		}

		chunks = append(chunks, Chunk{
			ID:         NewChunkID(filePath, start+1),
			FilePath:   filePath,
			StartLine:  start + 1,
			EndLine:    end + 1,
			Content:    content,
			Language:   language,
			Symbol:     b.symbol,
			SymbolKind: b.kind,
		})
	}

	return chunks
}

type symbolBoundary struct {
	start  int
	end    int
	symbol string
	kind   string
}
