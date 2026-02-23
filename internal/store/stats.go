package store

import (
	"context"
	"time"
)

type Stats struct {
	Files          int
	Chunks         int
	FailedFiles    int
	ActiveMemories int
	LastIndexed    time.Time
}

func (db *DB) Stats() (*Stats, error) {
	return db.StatsWithContext(context.Background())
}

func (db *DB) StatsWithContext(ctx context.Context) (*Stats, error) {
	s := &Stats{}

	db.conn.QueryRowContext(ctx, `SELECT COUNT(DISTINCT file_path) FROM chunks`).Scan(&s.Files)
	db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&s.Chunks)
	db.conn.QueryRowContext(ctx, `SELECT COUNT(DISTINCT file_path) FROM index_failures`).Scan(&s.FailedFiles)
	db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories WHERE expires_at > CURRENT_TIMESTAMP`).Scan(&s.ActiveMemories)
	db.conn.QueryRowContext(ctx, `SELECT MAX(indexed_at) FROM chunks`).Scan(&s.LastIndexed)

	return s, nil
}
