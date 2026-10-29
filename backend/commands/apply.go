package commands

import (
	"database/sql"
	"fmt"
	"strings"
)

// validateEntry reports whether an entry is still unwindable. Rows present
// in both images were updated by the command and must still exist; rows the
// command deleted or created are tolerated either way (LIFO discipline plus
// Purge keep those consistent), and a restore that hits a conflict fails
// loudly instead.
func (b *Bus) validateEntry(entry *Entry) error {
	for _, table := range entry.Memento.Tables {
		before := entry.Memento.Before[table]
		after := entry.Memento.After[table]
		for id := range before {
			if _, updated := after[id]; !updated {
				continue // deleted by the command; restore is an insert
			}
			var one int
			err := b.db.QueryRow(fmt.Sprintf("SELECT 1 FROM %s WHERE id = ?", table), id).Scan(&one)
			if err == sql.ErrNoRows {
				return fmt.Errorf("%s #%d no longer exists", table, id)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// apply restores one image side of a memento in a single transaction.
//
// target is the image to restore (Before for undo, After for redo), opposite
// the other side. Per table, in capture order so parents restore before
// children:
//
//   - row in both images  -> UPDATE only the differing columns (diff-based
//     restore: interleaved non-undoable writes to other columns survive)
//   - row only in target  -> INSERT with its original id (deleted rows come
//     back with their old ids, so FK children snap back consistently)
//
// Then, in reverse capture order so children delete before parents, rows
// only in the opposite image (created by the command) are deleted.
//
// Plain INSERT (not INSERT OR REPLACE) is deliberate: if the row somehow
// exists again the restore fails loudly and the entry is skipped, whereas
// OR REPLACE would cascade-delete the existing row's children.
func (b *Bus) apply(target, opposite map[string]map[int64]Row, tables []string) error {
	tx, err := b.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning restore transaction: %w", err)
	}
	defer tx.Rollback()

	for _, table := range tables {
		rows := target[table]
		other := opposite[table]
		for _, row := range sortedRows(rows) {
			if before, ok := other[row.ID]; ok {
				if err := updateDiffingColumns(tx, table, before, row); err != nil {
					return err
				}
				continue
			}
			if err := insertRow(tx, table, row); err != nil {
				return err
			}
		}
	}

	for i := len(tables) - 1; i >= 0; i-- {
		table := tables[i]
		for id := range opposite[table] {
			if _, kept := target[table][id]; !kept {
				if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE id = ?", table), id); err != nil {
					return fmt.Errorf("deleting %s #%d: %w", table, id, err)
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing restore: %w", err)
	}
	return nil
}

func sortedRows(rows map[int64]Row) []Row {
	ordered := make([]Row, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	for _, id := range ids {
		ordered = append(ordered, rows[id])
	}
	return ordered
}

// updateDiffingColumns restores only the columns that differ between the two
// images of an updated row. Column ownership keeps the diff safe: undoable
// commands never write columns owned by non-undoable paths (status_code and
// response_id belong to the send flow), so an interleaved send is never
// clobbered by an undo.
func updateDiffingColumns(tx *sql.Tx, table string, current, restore Row) error {
	columns := sortedColumnNames(restore.Values)

	var sets []string
	var args []any
	for _, col := range columns {
		if col == "id" {
			continue
		}
		old, oldOK := current.Values[col]
		new, newOK := restore.Values[col]
		// A column missing from the older image (schema migration added it
		// later) is treated as differing so the captured value is restored.
		if oldOK && newOK && equalValues(old, new) {
			continue
		}
		sets = append(sets, fmt.Sprintf("%s = ?", col))
		var value any
		if newOK {
			value = new
		}
		args = append(args, value)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, restore.ID)

	query := fmt.Sprintf("UPDATE %s SET %s WHERE id = ?", table, strings.Join(sets, ", "))
	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("restoring %s #%d: %w", table, restore.ID, err)
	}
	return nil
}

func insertRow(tx *sql.Tx, table string, row Row) error {
	columns := sortedColumnNames(row.Values)
	if len(columns) == 0 {
		return nil
	}

	placeholders := make([]string, len(columns))
	args := make([]any, len(columns))
	for i, col := range columns {
		placeholders[i] = "?"
		args[i] = row.Values[col]
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		table, strings.Join(columns, ", "), strings.Join(placeholders, ", "),
	)
	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("restoring %s #%d: %w", table, row.ID, err)
	}
	return nil
}

func sortedColumnNames(values map[string]any) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

// equalValues compares two column images. SQLite drivers may return TEXT as
// string or []byte depending on path, so bytes and strings are compared by
// content.
func equalValues(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if as, ok := a.(string); ok {
		if bs, ok2 := b.(string); ok2 {
			return as == bs
		}
		if bb, ok2 := b.([]byte); ok2 {
			return as == string(bb)
		}
		return false
	}
	if ab, ok := a.([]byte); ok {
		if bs, ok2 := b.(string); ok2 {
			return string(ab) == bs
		}
		if bb, ok2 := b.([]byte); ok2 {
			return string(ab) == string(bb)
		}
		return false
	}
	return a == b
}
