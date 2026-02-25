package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type LanguageStat struct {
	Language string `json:"language"`
	Chunks   int    `json:"chunks"`
	Files    int    `json:"files"`
}

type MemoryProposalStats struct {
	Pending   int `json:"pending"`
	Published int `json:"published"`
	Rejected  int `json:"rejected"`
}

type MemoryLifecycleStats struct {
	Archived         int `json:"archived"`
	ExpiredPublished int `json:"expired_published"`
}

type FailureWindowStats struct {
	Last24h     int       `json:"last_24h"`
	Last7d      int       `json:"last_7d"`
	LastFailure time.Time `json:"last_failure_at"`
}

type Stats struct {
	Files          int
	Chunks         int
	FailedFiles    int
	ActiveMemories int
	LastIndexed    time.Time

	EmbeddedChunks       int
	EmbeddingCoveragePct float64
	AvgChunksPerFile     float64
	TopLanguages         []LanguageStat

	MemoryProposals MemoryProposalStats
	Memories        MemoryLifecycleStats
	Failures        FailureWindowStats
}

func (db *DB) Stats() (*Stats, error) {
	return db.StatsWithContext(context.Background())
}

func (db *DB) StatsWithContext(ctx context.Context) (*Stats, error) {
	s := &Stats{}

	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(DISTINCT file_path) FROM chunks`).Scan(&s.Files); err != nil {
		return nil, err
	}
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&s.Chunks); err != nil {
		return nil, err
	}
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(DISTINCT file_path) FROM index_failures`).Scan(&s.FailedFiles); err != nil {
		return nil, err
	}
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories WHERE status='published' AND datetime(expires_at) > CURRENT_TIMESTAMP`).Scan(&s.ActiveMemories); err != nil {
		return nil, err
	}
	if err := scanNullTime(db.conn.QueryRowContext(ctx, `SELECT MAX(indexed_at) FROM chunks`), &s.LastIndexed); err != nil {
		return nil, err
	}

	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings`).Scan(&s.EmbeddedChunks); err != nil {
		return nil, err
	}
	if s.Chunks > 0 {
		s.EmbeddingCoveragePct = (float64(s.EmbeddedChunks) / float64(s.Chunks)) * 100
	}
	if s.Files > 0 {
		s.AvgChunksPerFile = float64(s.Chunks) / float64(s.Files)
	}

	rows, err := db.conn.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(language, ''), 'unknown') AS lang,
		       COUNT(*) AS chunks,
		       COUNT(DISTINCT file_path) AS files
		FROM chunks
		GROUP BY lang
		ORDER BY chunks DESC, lang ASC
		LIMIT 5
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var lang LanguageStat
		if err := rows.Scan(&lang.Language, &lang.Chunks, &lang.Files); err != nil {
			return nil, err
		}
		s.TopLanguages = append(s.TopLanguages, lang)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_proposals WHERE status='pending'`).Scan(&s.MemoryProposals.Pending); err != nil {
		return nil, err
	}
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_proposals WHERE status='published'`).Scan(&s.MemoryProposals.Published); err != nil {
		return nil, err
	}
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_proposals WHERE status='rejected'`).Scan(&s.MemoryProposals.Rejected); err != nil {
		return nil, err
	}

	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories WHERE status='archived'`).Scan(&s.Memories.Archived); err != nil {
		return nil, err
	}
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories WHERE status='published' AND datetime(expires_at) <= CURRENT_TIMESTAMP`).Scan(&s.Memories.ExpiredPublished); err != nil {
		return nil, err
	}

	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM index_failures WHERE created_at >= datetime('now', '-24 hours')`).Scan(&s.Failures.Last24h); err != nil {
		return nil, err
	}
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM index_failures WHERE created_at >= datetime('now', '-7 days')`).Scan(&s.Failures.Last7d); err != nil {
		return nil, err
	}
	if err := scanNullTime(db.conn.QueryRowContext(ctx, `SELECT MAX(created_at) FROM index_failures`), &s.Failures.LastFailure); err != nil {
		return nil, err
	}

	return s, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanNullTime(row rowScanner, out *time.Time) error {
	var raw interface{}
	if err := row.Scan(&raw); err != nil {
		return err
	}
	if raw == nil {
		*out = time.Time{}
		return nil
	}

	switch v := raw.(type) {
	case time.Time:
		*out = v
		return nil
	case string:
		ts, err := parseSQLiteTime(v)
		if err != nil {
			return err
		}
		*out = ts
		return nil
	case []byte:
		ts, err := parseSQLiteTime(string(v))
		if err != nil {
			return err
		}
		*out = ts
		return nil
	default:
		return fmt.Errorf("unsupported time value type %T", raw)
	}
}

func parseSQLiteTime(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	// Some drivers may serialize time.Time with monotonic clock suffix (" m=...").
	// Strip that suffix before parsing.
	if i := strings.Index(v, " m="); i >= 0 {
		v = v[:i]
	}
	layouts := []string{
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	}
	var lastErr error
	for _, layout := range layouts {
		ts, err := time.Parse(layout, v)
		if err == nil {
			return ts, nil
		}
		lastErr = err
	}
	if unixSec, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Unix(unixSec, 0), nil
	}
	return time.Time{}, fmt.Errorf("parse sqlite time %q: %w", v, lastErr)
}
