package storage

import (
	"database/sql"
	"os"
)

type mediaTrimCandidate struct {
	name string
	size int64
}

func (s *LocalMediaCacheStore) enforceLimitLocked(db *sql.DB, mediaType MediaType, protected map[string]struct{}) error {
	maxBytes := int64(s.limitBytes(mediaType))
	if maxBytes <= 0 {
		return nil
	}
	usage, err := usageBytes(db, mediaType)
	if err != nil || usage <= maxBytes {
		return err
	}
	if protected == nil {
		protected = map[string]struct{}{}
	}
	if newest, err := newestName(db, mediaType); err != nil {
		return err
	} else if newest != "" {
		protected[newest] = struct{}{}
	}
	target := int64(float64(maxBytes) * lowWatermarkRatio)
	candidates, err := mediaTrimCandidates(db, mediaType)
	if err != nil {
		return err
	}
	for _, item := range candidates {
		if usage <= target {
			break
		}
		if _, ok := protected[item.name]; ok {
			continue
		}
		path, err := s.pathForName(mediaType, item.name)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if _, err := db.Exec(`
			DELETE FROM local_media_files
			WHERE media_type = ? AND name = ?
		`, string(mediaType), item.name); err != nil {
			return err
		}
		usage -= item.size
		if usage < 0 {
			usage = 0
		}
	}
	return nil
}

func mediaTrimCandidates(db *sql.DB, mediaType MediaType) ([]mediaTrimCandidate, error) {
	rows, err := db.Query(`
		SELECT name, size_bytes FROM local_media_files
		WHERE media_type = ?
		ORDER BY created_at_ns ASC, name ASC
	`, string(mediaType))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := []mediaTrimCandidate{}
	for rows.Next() {
		item := mediaTrimCandidate{}
		if err := rows.Scan(&item.name, &item.size); err != nil {
			return nil, err
		}
		candidates = append(candidates, item)
	}
	return candidates, rows.Err()
}

func (s *LocalMediaCacheStore) deleteIndexRowIfPresent(mediaType MediaType, name string) error {
	dbPath, err := LocalMediaCacheDBPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	db, err := s.connectIndex()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`
		DELETE FROM local_media_files
		WHERE media_type = ? AND name = ?
	`, string(mediaType), name)
	return err
}

func (s *LocalMediaCacheStore) deleteIndexRowsIfPresent(mediaType MediaType) error {
	dbPath, err := LocalMediaCacheDBPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	db, err := s.connectIndex()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DELETE FROM local_media_files WHERE media_type = ?`, string(mediaType))
	return err
}
