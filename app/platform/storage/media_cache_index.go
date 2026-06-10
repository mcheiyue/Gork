package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const (
	localMediaTable   = "local_media_files"
	lowWatermarkRatio = 0.60
)

func (s *LocalMediaCacheStore) connectIndex() (*sql.DB, error) {
	dbPath, err := LocalMediaCacheDBPath()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA synchronous=NORMAL`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureMediaCacheSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func ensureMediaCacheSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS local_media_files (
			media_type    TEXT    NOT NULL,
			name          TEXT    NOT NULL,
			size_bytes    INTEGER NOT NULL,
			created_at_ns INTEGER NOT NULL,
			updated_at_ns INTEGER NOT NULL,
			PRIMARY KEY (media_type, name)
		);
		CREATE INDEX IF NOT EXISTS idx_local_media_order
			ON local_media_files (media_type, created_at_ns, name);
	`)
	return err
}

func (s *LocalMediaCacheStore) saveIndexed(mediaType MediaType, path string, raw []byte) error {
	db, err := s.connectIndex()
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := os.Stat(path); err == nil {
		if err := s.upsertExistingRow(db, mediaType, path); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		if err := writeIfMissing(path, raw); err != nil {
			return err
		}
		if err := s.upsertNewRow(db, mediaType, path); err != nil {
			return err
		}
	} else {
		return err
	}
	return s.enforceLimitLocked(db, mediaType, map[string]struct{}{filepath.Base(path): {}})
}

func (s *LocalMediaCacheStore) reconcileIndexed(mediaType MediaType) error {
	db, err := s.connectIndex()
	if err != nil {
		return err
	}
	defer db.Close()
	dir, err := s.mediaDir(mediaType)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM local_media_files WHERE media_type = ?`, string(mediaType)); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !s.allowed(mediaType, filepath.Ext(entry.Name())) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		stat, err := os.Stat(path)
		if err != nil {
			continue
		}
		ts := stat.ModTime().UnixNano()
		if _, err := db.Exec(`
			INSERT INTO local_media_files (
				media_type, name, size_bytes, created_at_ns, updated_at_ns
			) VALUES (?, ?, ?, ?, ?)
		`, string(mediaType), entry.Name(), stat.Size(), ts, ts); err != nil {
			return err
		}
	}
	return s.enforceLimitLocked(db, mediaType, nil)
}

func (s *LocalMediaCacheStore) upsertExistingRow(db *sql.DB, mediaType MediaType, path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	fallback := stat.ModTime().UnixNano()
	created, err := lookupCreatedAt(db, mediaType, filepath.Base(path), fallback)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		INSERT INTO local_media_files (
			media_type, name, size_bytes, created_at_ns, updated_at_ns
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(media_type, name) DO UPDATE SET
			size_bytes = excluded.size_bytes,
			updated_at_ns = excluded.updated_at_ns
	`, string(mediaType), filepath.Base(path), stat.Size(), created, stat.ModTime().UnixNano())
	return err
}

func (s *LocalMediaCacheStore) upsertNewRow(db *sql.DB, mediaType MediaType, path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	now := time.Now().UnixNano()
	_, err = db.Exec(`
		INSERT INTO local_media_files (
			media_type, name, size_bytes, created_at_ns, updated_at_ns
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(media_type, name) DO UPDATE SET
			size_bytes = excluded.size_bytes,
			created_at_ns = excluded.created_at_ns,
			updated_at_ns = excluded.updated_at_ns
	`, string(mediaType), filepath.Base(path), stat.Size(), now, now)
	return err
}

func lookupCreatedAt(db *sql.DB, mediaType MediaType, name string, fallback int64) (int64, error) {
	var created int64
	err := db.QueryRow(`
		SELECT created_at_ns FROM local_media_files
		WHERE media_type = ? AND name = ?
	`, string(mediaType), name).Scan(&created)
	if err == sql.ErrNoRows {
		return fallback, nil
	}
	return created, err
}

func usageBytes(db *sql.DB, mediaType MediaType) (int64, error) {
	var total int64
	err := db.QueryRow(`
		SELECT COALESCE(SUM(size_bytes), 0) FROM local_media_files
		WHERE media_type = ?
	`, string(mediaType)).Scan(&total)
	return total, err
}

func newestName(db *sql.DB, mediaType MediaType) (string, error) {
	var name string
	err := db.QueryRow(`
		SELECT name FROM local_media_files
		WHERE media_type = ?
		ORDER BY created_at_ns DESC, name DESC
		LIMIT 1
	`, string(mediaType)).Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}
