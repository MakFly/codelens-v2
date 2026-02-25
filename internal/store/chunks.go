package store

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

type ChunkRecord struct {
	ID         string
	FilePath   string
	StartLine  int
	EndLine    int
	Content    string
	Language   string
	Symbol     string
	SymbolKind string
	Hash       string
}

// UpsertChunk inserts or replaces a chunk and its embedding.
func (db *DB) UpsertChunk(ctx context.Context, chunk ChunkRecord, embedding []float32) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO chunks (id, file_path, start_line, end_line, content, language, symbol, symbol_kind, hash, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			content=excluded.content, hash=excluded.hash, indexed_at=excluded.indexed_at,
			symbol=excluded.symbol, symbol_kind=excluded.symbol_kind
	`, chunk.ID, chunk.FilePath, chunk.StartLine, chunk.EndLine,
		chunk.Content, chunk.Language, chunk.Symbol, chunk.SymbolKind,
		chunk.Hash, time.Now())
	if err != nil {
		return fmt.Errorf("upsert chunk: %w", err)
	}

	vectorBlob := float32SliceToBlob(embedding)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO embeddings (chunk_id, vector)
		VALUES (?, ?)
		ON CONFLICT(chunk_id) DO UPDATE SET vector=excluded.vector
	`, chunk.ID, vectorBlob)
	if err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}

	return tx.Commit()
}

// DeleteChunksByFile removes all chunks for a given file path.
func (db *DB) DeleteChunksByFile(ctx context.Context, filePath string) error {
	_, err := db.conn.ExecContext(ctx, `DELETE FROM chunks WHERE file_path = ?`, filePath)
	return err
}

// GetFileHash returns the last recorded hash for a file.
func (db *DB) GetFileHash(ctx context.Context, filePath string) (string, error) {
	var hash string
	err := db.conn.QueryRowContext(ctx,
		`SELECT hash FROM file_hashes WHERE file_path = ?`, filePath,
	).Scan(&hash)
	if err != nil {
		return "", err // sql.ErrNoRows if not found
	}
	return hash, nil
}

// SetFileHash stores the hash for a file.
func (db *DB) SetFileHash(ctx context.Context, filePath, hash string) error {
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO file_hashes (file_path, hash, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(file_path) DO UPDATE SET hash=excluded.hash, updated_at=excluded.updated_at
	`, filePath, hash)
	return err
}

// LoadAllEmbeddings loads all chunk IDs and their embeddings for HNSW index bootstrap.
type EmbeddingRecord struct {
	ChunkID   string
	Embedding []float32
}

func (db *DB) LoadAllEmbeddings(ctx context.Context) ([]EmbeddingRecord, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT chunk_id, vector FROM embeddings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []EmbeddingRecord
	for rows.Next() {
		var r EmbeddingRecord
		var blob []byte
		if err := rows.Scan(&r.ChunkID, &blob); err != nil {
			return nil, err
		}
		r.Embedding = blobToFloat32Slice(blob)
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetChunksByIDs fetches chunk metadata for a list of IDs.
func (db *DB) GetChunksByIDs(ctx context.Context, ids []string) ([]ChunkRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build IN clause
	placeholders := make([]interface{}, len(ids))
	query := "SELECT id, file_path, start_line, end_line, content, language, symbol, symbol_kind, hash FROM chunks WHERE id IN ("
	for i, id := range ids {
		if i > 0 {
			query += ","
		}
		query += "?"
		placeholders[i] = id
	}
	query += ")"

	rows, err := db.conn.QueryContext(ctx, query, placeholders...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.ID, &c.FilePath, &c.StartLine, &c.EndLine,
			&c.Content, &c.Language, &c.Symbol, &c.SymbolKind, &c.Hash); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// LoadAllChunks loads all indexed chunks (without embeddings).
func (db *DB) LoadAllChunks(ctx context.Context) ([]ChunkRecord, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT id, file_path, start_line, end_line, content, language, symbol, symbol_kind, hash
		FROM chunks
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.ID, &c.FilePath, &c.StartLine, &c.EndLine,
			&c.Content, &c.Language, &c.Symbol, &c.SymbolKind, &c.Hash); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// --- Serialization helpers ---

func float32SliceToBlob(f []float32) []byte {
	b := make([]byte, len(f)*4)
	for i, v := range f {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}
	return b
}

func blobToFloat32Slice(b []byte) []float32 {
	f := make([]float32, len(b)/4)
	for i := range f {
		f[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return f
}
