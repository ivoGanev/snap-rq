package commands

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// This file is the only clever part of the undo system: putting saved rows
// back. Everything else just saves copies.

// rowUpdate writes some columns back onto one row.
type rowUpdate struct {
	table   string
	id      int64
	columns map[string]any // the values to write back
}

// restoration is the plan for one undo or redo.
type restoration struct {
	updates []rowUpdate // write these columns back onto these rows
	inserts []SavedRow  // insert these rows again, exactly as saved
	deletes []SavedRow  // delete these rows again
}

// planRestore works out how to return the database to the "want" state,
// given the "other" state it currently matches:
//
//	row in both lists  → some columns differ → update those columns only
//	row only in "want" → it was deleted → insert it back with its old id
//	row only in "other" → it was created → delete it
//
// Updating only the changed columns is what lets a request send that
// happened in the middle survive: the send writes status_code, the undo
// writes back only the name, and both live happily on the same row.
func planRestore(want, other []SavedRow) restoration {
	plan := restoration{
		updates: []rowUpdate{},
		inserts: []SavedRow{},
		deletes: []SavedRow{},
	}

	otherByTable := rowsByTable(other)
	wantByTable := rowsByTable(want)

	for _, row := range want {
		counterpart, existed := otherByTable[row.Table][row.ID]
		switch {
		case !existed:
			// The command deleted this row; bring it back as it was.
			plan.inserts = append(plan.inserts, row)

		default:
			// The command changed this row; restore only what differs.
			columns := changedColumns(row, counterpart)
			if len(columns) > 0 {
				plan.updates = append(plan.updates, rowUpdate{
					table:   row.Table,
					id:      row.ID,
					columns: columns,
				})
			}
		}
	}

	for _, row := range other {
		if _, stillWanted := wantByTable[row.Table][row.ID]; !stillWanted {
			// The command created this row; remove it again.
			plan.deletes = append(plan.deletes, row)
		}
	}

	return plan
}

// changedColumns returns the columns of "row" that disagree with its
// counterpart. A column the older copy does not have (the schema gained
// it since) always counts as changed, so the saved value is written back.
func changedColumns(row, counterpart SavedRow) map[string]any {
	changed := map[string]any{}
	for column, value := range row.Columns {
		old, had := counterpart.Columns[column]
		if !had || !sameValue(old, value) {
			changed[column] = value
		}
	}
	return changed
}

// sameValue compares two saved column values.
func sameValue(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a == b
}

func rowsByTable(rows []SavedRow) map[string]map[int64]SavedRow {
	byTable := make(map[string]map[int64]SavedRow, 8)
	for _, row := range rows {
		if byTable[row.Table] == nil {
			byTable[row.Table] = map[int64]SavedRow{}
		}
		byTable[row.Table][row.ID] = row
	}
	return byTable
}

// rowsStillExist checks that the rows a step changed are still there.
// Rows it deleted or created are fine either way — they get inserted or
// removed again. If a changed row was destroyed for good since, undoing is
// impossible.
func rowsStillExist(db *sql.DB, step *UndoStep) error {
	afterByTable := rowsByTable(step.After)
	for _, row := range step.Before {
		if _, wasChanged := afterByTable[row.Table][row.ID]; !wasChanged {
			continue // the command deleted this row; undo re-inserts it
		}
		var one int
		err := db.QueryRow(
			fmt.Sprintf("SELECT 1 FROM %s WHERE id = ?", row.Table), row.ID,
		).Scan(&one)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%s #%d no longer exists", row.Table, row.ID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// putRowsBack returns the database to the "want" state. Everything happens
// in one transaction, so a failure changes nothing at all.
func putRowsBack(db *sql.DB, want, other []SavedRow) error {
	plan := planRestore(want, other)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("starting the restore transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert parents first: the saved order is parents before children.
	// A plain INSERT (not INSERT OR REPLACE) on purpose — if the row is
	// somehow back already, failing loudly beats silently destroying the
	// rows that point at it.
	for _, row := range plan.inserts {
		if err := insertRow(tx, row); err != nil {
			return err
		}
	}

	for _, update := range plan.updates {
		if err := updateColumns(tx, update); err != nil {
			return err
		}
	}

	// Delete children first: the reverse of the saved order.
	for i := len(plan.deletes) - 1; i >= 0; i-- {
		row := plan.deletes[i]
		if _, err := tx.Exec(
			fmt.Sprintf("DELETE FROM %s WHERE id = ?", row.Table), row.ID,
		); err != nil {
			return fmt.Errorf("deleting %s #%d again: %w", row.Table, row.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing the restore: %w", err)
	}
	return nil
}

func insertRow(tx *sql.Tx, row SavedRow) error {
	columns := sortedColumnNames(row.Columns)
	if len(columns) == 0 {
		return nil
	}

	placeholders := make([]string, len(columns))
	args := make([]any, len(columns))
	for i, column := range columns {
		placeholders[i] = "?"
		args[i] = row.Columns[column]
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		row.Table, strings.Join(columns, ", "), strings.Join(placeholders, ", "),
	)
	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("inserting %s #%d back: %w", row.Table, row.ID, err)
	}
	return nil
}

func updateColumns(tx *sql.Tx, update rowUpdate) error {
	columns := sortedColumnNames(update.columns)

	sets := make([]string, len(columns))
	args := make([]any, 0, len(columns)+1)
	for i, column := range columns {
		sets[i] = column + " = ?"
		args = append(args, update.columns[column])
	}
	args = append(args, update.id)

	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE id = ?",
		update.table, strings.Join(sets, ", "),
	)
	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("restoring %s #%d: %w", update.table, update.id, err)
	}
	return nil
}

// sortedColumnNames gives SQL a deterministic column order, which makes
// the queries reproducible in tests and logs.
func sortedColumnNames(columns map[string]any) []string {
	names := make([]string, 0, len(columns))
	for name := range columns {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
