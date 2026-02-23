package store

import (
	"context"
	"strings"
)

func (db *DB) RecordIndexFailure(ctx context.Context, filePath, errMsg string) error {
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO index_failures (file_path, error, created_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
	`, filePath, errMsg)
	return err
}

func (db *DB) ClearIndexFailuresByFile(ctx context.Context, filePath string) error {
	_, err := db.conn.ExecContext(ctx, `DELETE FROM index_failures WHERE file_path = ?`, filePath)
	return err
}

func (db *DB) PurgeExcludedByPrefixes(ctx context.Context, prefixes []string) error {
	for _, p := range prefixes {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		like := p + "%"
		if _, err := db.conn.ExecContext(ctx, `DELETE FROM chunks WHERE lower(file_path) LIKE ?`, like); err != nil {
			return err
		}
		if _, err := db.conn.ExecContext(ctx, `DELETE FROM file_hashes WHERE lower(file_path) LIKE ?`, like); err != nil {
			return err
		}
		if _, err := db.conn.ExecContext(ctx, `DELETE FROM index_failures WHERE lower(file_path) LIKE ?`, like); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) PurgeExcludedBySuffixes(ctx context.Context, suffixes []string) error {
	for _, s := range suffixes {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		like := "%" + s
		if _, err := db.conn.ExecContext(ctx, `DELETE FROM chunks WHERE lower(file_path) LIKE ?`, like); err != nil {
			return err
		}
		if _, err := db.conn.ExecContext(ctx, `DELETE FROM file_hashes WHERE lower(file_path) LIKE ?`, like); err != nil {
			return err
		}
		if _, err := db.conn.ExecContext(ctx, `DELETE FROM index_failures WHERE lower(file_path) LIKE ?`, like); err != nil {
			return err
		}
	}
	return nil
}
