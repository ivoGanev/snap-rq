package services

import (
	"database/sql"
	"fmt"
	"strings"
)

// IconService performs cross-table cleanup of icon appearances.
type IconService struct {
	db *sql.DB
}

func NewIconService(db *sql.DB) *IconService {
	return &IconService{db: db}
}

// CleanupIconReferences resets every icon appearance whose id is not in
// validIDs to defaultID. Colour appearances are untouched. Idempotent: once
// nothing references a removed id, running again changes nothing.
func (s *IconService) CleanupIconReferences(validIDs []string, defaultID string) error {
	if len(validIDs) == 0 {
		return fmt.Errorf("valid ids are required")
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(validIDs)), ",")
	args := make([]any, 0, len(validIDs)+1)
	args = append(args, defaultID)
	for _, id := range validIDs {
		args = append(args, id)
	}

	tables := []string{"collection_appearances", "favourite_appearances", "tag_appearances"}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning icon cleanup transaction: %w", err)
	}
	defer tx.Rollback()

	for _, table := range tables {
		query := fmt.Sprintf(
			"UPDATE %s SET appearance_value = ? WHERE appearance_type = 'icon' AND appearance_value NOT IN (%s)",
			table, placeholders,
		)
		if _, err := tx.Exec(query, args...); err != nil {
			return fmt.Errorf("cleaning %s icon ids: %w", table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing icon cleanup transaction: %w", err)
	}
	return nil
}
