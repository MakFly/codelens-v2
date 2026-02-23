package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/yourusername/codelens/internal/jit"
)

const defaultMemoryTTL = 28 * 24 * time.Hour

// SaveMemory stores a published memory with its citations.
func (db *DB) SaveMemory(ctx context.Context, insight string, citations []jit.Citation) (string, error) {
	return db.SavePublishedMemory(ctx, insight, "insight", citations)
}

func (db *DB) SavePublishedMemory(ctx context.Context, insight, memoryType string, citations []jit.Citation) (string, error) {
	id := fmt.Sprintf("mem_%x", time.Now().UnixNano())
	expiresAt := time.Now().Add(defaultMemoryTTL)

	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO memories (id, insight, memory_type, status, published_at, expires_at)
		VALUES (?, ?, ?, 'published', CURRENT_TIMESTAMP, ?)
	`, id, insight, memoryType, expiresAt)
	if err != nil {
		return "", fmt.Errorf("insert memory: %w", err)
	}

	for _, c := range citations {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO citations (memory_id, file_path, line_start, line_end, hash)
			VALUES (?, ?, ?, ?, ?)
		`, id, c.FilePath, c.LineStart, c.LineEnd, c.Hash)
		if err != nil {
			return "", fmt.Errorf("insert citation: %w", err)
		}
	}

	return id, tx.Commit()
}

// Memory with its citations, for recall.
type MemoryRecord struct {
	ID        string
	Insight   string
	Type      string
	Citations []jit.Citation
	CreatedAt time.Time
	HitCount  int
}

type MemoryProposalRecord struct {
	ID         string
	Insight    string
	Type       string
	Status     string
	Reason     string
	Citations  []jit.Citation
	CreatedAt  time.Time
	ReviewedAt sql.NullTime
}

func (db *DB) SaveMemoryProposal(ctx context.Context, insight, memoryType string, citations []jit.Citation) (string, error) {
	id := fmt.Sprintf("proposal_%x", time.Now().UnixNano())

	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO memory_proposals (id, insight, memory_type, status)
		VALUES (?, ?, ?, 'pending')
	`, id, insight, memoryType)
	if err != nil {
		return "", fmt.Errorf("insert memory proposal: %w", err)
	}

	for _, c := range citations {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO proposal_citations (proposal_id, file_path, line_start, line_end, hash)
			VALUES (?, ?, ?, ?, ?)
		`, id, c.FilePath, c.LineStart, c.LineEnd, c.Hash)
		if err != nil {
			return "", fmt.Errorf("insert proposal citation: %w", err)
		}
	}

	return id, tx.Commit()
}

func (db *DB) GetMemoryProposal(ctx context.Context, proposalID string) (*MemoryProposalRecord, error) {
	var p MemoryProposalRecord
	err := db.conn.QueryRowContext(ctx, `
		SELECT id, insight, memory_type, status, COALESCE(reason, ''), created_at, reviewed_at
		FROM memory_proposals
		WHERE id = ?
	`, proposalID).Scan(&p.ID, &p.Insight, &p.Type, &p.Status, &p.Reason, &p.CreatedAt, &p.ReviewedAt)
	if err != nil {
		return nil, err
	}

	citations, err := db.loadProposalCitations(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	p.Citations = citations
	return &p, nil
}

func (db *DB) RejectMemoryProposal(ctx context.Context, proposalID, reason string) error {
	res, err := db.conn.ExecContext(ctx, `
		UPDATE memory_proposals
		SET status = 'rejected', reason = ?, reviewed_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'pending'
	`, reason, proposalID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("proposal not found or not pending")
	}
	return nil
}

func (db *DB) PublishMemoryProposal(ctx context.Context, proposalID string) (string, error) {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var insight string
	var memoryType string
	err = tx.QueryRowContext(ctx, `
		SELECT insight, memory_type
		FROM memory_proposals
		WHERE id = ? AND status = 'pending'
	`, proposalID).Scan(&insight, &memoryType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("proposal not found or not pending")
		}
		return "", err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT file_path, line_start, line_end, hash
		FROM proposal_citations
		WHERE proposal_id = ?
	`, proposalID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var citations []jit.Citation
	for rows.Next() {
		var c jit.Citation
		if err := rows.Scan(&c.FilePath, &c.LineStart, &c.LineEnd, &c.Hash); err != nil {
			return "", err
		}
		citations = append(citations, c)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	memoryID := fmt.Sprintf("mem_%x", time.Now().UnixNano())
	expiresAt := time.Now().Add(defaultMemoryTTL)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO memories (id, insight, memory_type, status, published_at, expires_at)
		VALUES (?, ?, ?, 'published', CURRENT_TIMESTAMP, ?)
	`, memoryID, insight, memoryType, expiresAt)
	if err != nil {
		return "", err
	}

	for _, c := range citations {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO citations (memory_id, file_path, line_start, line_end, hash)
			VALUES (?, ?, ?, ?, ?)
		`, memoryID, c.FilePath, c.LineStart, c.LineEnd, c.Hash)
		if err != nil {
			return "", err
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE memory_proposals
		SET status = 'published', reviewed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, proposalID)
	if err != nil {
		return "", err
	}

	return memoryID, tx.Commit()
}

// LoadActiveMemories returns all non-expired memories with their citations.
func (db *DB) LoadActiveMemories(ctx context.Context) ([]MemoryRecord, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT id, insight, memory_type, created_at, hit_count
		FROM memories
		WHERE status = 'published' AND expires_at > CURRENT_TIMESTAMP
		ORDER BY hit_count DESC, created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []MemoryRecord
	for rows.Next() {
		var m MemoryRecord
		if err := rows.Scan(&m.ID, &m.Insight, &m.Type, &m.CreatedAt, &m.HitCount); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load citations for each memory
	for i := range memories {
		cites, err := db.loadCitations(ctx, memories[i].ID)
		if err != nil {
			return nil, err
		}
		memories[i].Citations = cites
	}

	return memories, nil
}

func (db *DB) loadCitations(ctx context.Context, memoryID string) ([]jit.Citation, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT file_path, line_start, line_end, hash
		FROM citations WHERE memory_id = ?
	`, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var citations []jit.Citation
	for rows.Next() {
		var c jit.Citation
		if err := rows.Scan(&c.FilePath, &c.LineStart, &c.LineEnd, &c.Hash); err != nil {
			return nil, err
		}
		citations = append(citations, c)
	}
	return citations, rows.Err()
}

func (db *DB) loadProposalCitations(ctx context.Context, proposalID string) ([]jit.Citation, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT file_path, line_start, line_end, hash
		FROM proposal_citations WHERE proposal_id = ?
	`, proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var citations []jit.Citation
	for rows.Next() {
		var c jit.Citation
		if err := rows.Scan(&c.FilePath, &c.LineStart, &c.LineEnd, &c.Hash); err != nil {
			return nil, err
		}
		citations = append(citations, c)
	}
	return citations, rows.Err()
}

// RecordMemoryHit updates hit count and last_used for a memory.
func (db *DB) RecordMemoryHit(ctx context.Context, memoryID string) error {
	_, err := db.conn.ExecContext(ctx, `
		UPDATE memories SET hit_count = hit_count + 1, last_used = CURRENT_TIMESTAMP
		WHERE id = ?
	`, memoryID)
	return err
}

// ArchiveMemory marks a memory as archived so it is no longer returned by recall.
func (db *DB) ArchiveMemory(ctx context.Context, memoryID string) error {
	_, err := db.conn.ExecContext(ctx, `
		UPDATE memories
		SET status = 'archived'
		WHERE id = ?
	`, memoryID)
	return err
}

// DeleteExpiredMemories removes expired memories (call periodically).
func (db *DB) DeleteExpiredMemories(ctx context.Context) (int64, error) {
	res, err := db.conn.ExecContext(ctx, `
		DELETE FROM memories WHERE status = 'published' AND expires_at <= CURRENT_TIMESTAMP
	`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
