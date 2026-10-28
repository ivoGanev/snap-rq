package services

import (
	"database/sql"
	"fmt"
	"strings"

	"snap-rq/backend/models"
)

// IconService handles cross-table operations on icon appearances.
type IconService struct {
	db *sql.DB
}

func NewIconService(db *sql.DB) *IconService {
	return &IconService{db: db}
}

// MigrateIconIds rewrites appearance icon ids across the three appearance
// tables using the given replacements. It is idempotent: once every old id has
// been replaced, running it again changes nothing. The frontend (owner of the
// icon manifest) computes the replacements and calls this once on startup.
func (s *IconService) MigrateIconIds(replacements []models.IconReplacement) error {
	if len(replacements) == 0 {
		return nil
	}

	var whens strings.Builder
	args := make([]any, 0, len(replacements)*2)
	for _, replacement := range replacements {
		whens.WriteString(" WHEN ? THEN ?")
		args = append(args, replacement.OldID, replacement.NewID)
	}
	caseExpr := "CASE appearance_value" + whens.String() + " ELSE appearance_value END"

	tables := []string{"collection_appearances", "favourite_appearances", "tag_appearances"}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning icon migration transaction: %w", err)
	}
	defer tx.Rollback()

	for _, table := range tables {
		query := fmt.Sprintf(
			"UPDATE %s SET appearance_value = %s WHERE appearance_type = 'icon'",
			table, caseExpr,
		)
		if _, err := tx.Exec(query, args...); err != nil {
			return fmt.Errorf("migrating %s icon ids: %w", table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing icon migration transaction: %w", err)
	}
	return nil
}
