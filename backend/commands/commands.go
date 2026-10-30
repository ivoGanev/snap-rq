// Package commands implements the app's undo system.
//
// How undo works:
//
//  1. A command runs inside a transaction.
//  2. Before it runs, we save copies of every row it will touch.
//     After it runs, we save copies again.
//  3. Undo puts the before-copies back. Redo puts the after-copies back.
//  4. That's it. There is no cleverer trick anywhere in this package.
//
// A command is a named function that changes the database, plus a label
// for the undo menu. Commands that cannot be taken back (sending a
// request, saving a response) set Undoable to false; they still run
// through the History so that every change follows the same path.
package commands

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Command is one named change the app can make.
type Command struct {
	// Name is the public identifier, e.g. "request.delete". One day an AI
	// agent or a CLI will trigger commands by this name.
	Name string
	// Label is what the user sees in the undo menu, e.g. "Delete Request".
	Label string
	// Undoable is false for changes we cannot take back: a request that
	// was sent, or history that must never be rewritten (responses).
	Undoable bool
	// Apply does the work. Use tx for SQL and rec to say which rows to
	// save copies of. The return value goes back to the caller unchanged.
	Apply func(tx *sql.Tx, rec *Recorder, input json.RawMessage) (any, error)
}

// SavedRow is a copy of one database row at a moment in time.
type SavedRow struct {
	Table   string
	ID      int64
	Columns map[string]any
}

// Touched notes what the user will see change, so the UI can refresh it.
// Only ids, never values — variable values can be secrets.
type Touched struct {
	Entity string  `json:"entity"` // e.g. "request"
	Op     string  `json:"op"`     // "create", "update" or "delete"
	IDs    []int64 `json:"ids"`
}

// tableNamePattern keeps table names to plain lowercase identifiers: they
// end up inside SQL strings, so nothing else is acceptable.
var tableNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Recorder saves copies of the rows a command is about to change.
//
// A command must tell the recorder about every row it touches:
//
//	rec.SaveRows("http_requests", id)     // rows that exist and will be
//	                                      // changed or deleted
//	rec.SaveNewRows("http_requests", id)  // rows the command just created
//
// Mention parent tables first: rows are put back in the order they were
// saved, so a parent is restored before the children that point at it.
type Recorder struct {
	tx *sql.Tx
	// First copies of the rows, kept per table and id so a row saved twice
	// keeps its first (pre-change) copy.
	saved map[string]map[int64]SavedRow
	// After copies, filled once the command has finished.
	after map[string]map[int64]SavedRow
	// Every (table, id) to copy again after the command finishes.
	reread map[string]map[int64]bool
	// Tables in the order they were first mentioned.
	tables  []string
	touched []Touched
}

func newRecorder(tx *sql.Tx) *Recorder {
	return &Recorder{
		tx:     tx,
		saved:  map[string]map[int64]SavedRow{},
		after:  map[string]map[int64]SavedRow{},
		reread: map[string]map[int64]bool{},
	}
}

func (r *Recorder) noteTable(table string) error {
	if !tableNamePattern.MatchString(table) {
		return fmt.Errorf("invalid table name %q", table)
	}
	if r.reread[table] == nil {
		r.reread[table] = map[int64]bool{}
		r.tables = append(r.tables, table)
	}
	return nil
}

// SaveRows copies the given rows as they are right now. Call it just
// before changing or deleting them. A row that does not exist gets no
// copy — together with SaveNewRows, that is how undo tells "changed"
// apart from "created".
func (r *Recorder) SaveRows(table string, ids ...int64) error {
	if len(ids) == 0 {
		return nil
	}
	if err := r.noteTable(table); err != nil {
		return err
	}

	rows, err := loadRows(r.tx, table, ids)
	if err != nil {
		return fmt.Errorf("saving copies of %s: %w", table, err)
	}
	if r.saved[table] == nil {
		r.saved[table] = map[int64]SavedRow{}
	}
	for id, row := range rows {
		if _, alreadySaved := r.saved[table][id]; !alreadySaved {
			r.saved[table][id] = row
		}
	}
	for _, id := range ids {
		r.reread[table][id] = true
	}
	return nil
}

// SaveNewRows remembers rows the command has just created, so undo will
// delete them again.
func (r *Recorder) SaveNewRows(table string, ids ...int64) error {
	if len(ids) == 0 {
		return nil
	}
	if err := r.noteTable(table); err != nil {
		return err
	}
	for _, id := range ids {
		r.reread[table][id] = true
	}
	return nil
}

// Touched notes what the user will see change.
func (r *Recorder) Touched(entity, op string, ids ...int64) {
	if len(ids) == 0 {
		return
	}
	r.touched = append(r.touched, Touched{Entity: entity, Op: op, IDs: ids})
}

// saveAfterCopies re-reads every noted row, still inside the command's
// transaction, so the after copies match exactly what gets committed.
func (r *Recorder) saveAfterCopies() error {
	for _, table := range r.tables {
		ids := make([]int64, 0, len(r.reread[table]))
		for id := range r.reread[table] {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

		rows, err := loadRows(r.tx, table, ids)
		if err != nil {
			return fmt.Errorf("saving after copies of %s: %w", table, err)
		}
		r.after[table] = rows
	}
	return nil
}

// rowsKey names the rows being watched, e.g. "http_requests:42". Two runs
// of the same command with the same rowsKey are the same edit repeated —
// that is how quick repeats are recognised as one undo step.
func (r *Recorder) rowsKey() string {
	parts := make([]string, 0, 16)
	for _, table := range r.tables {
		ids := make([]int64, 0, len(r.reread[table]))
		for id := range r.reread[table] {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			parts = append(parts, table+":"+strconv.FormatInt(id, 10))
		}
	}
	return strings.Join(parts, " ")
}

// step gathers everything the recorder saw into one undo step. Rows are
// listed parents-first, in the order their tables were mentioned.
func (r *Recorder) step(cmd *Command) *UndoStep {
	step := &UndoStep{
		Command: cmd.Name,
		Label:   cmd.Label,
		When:    time.Now(),
		Touched: r.touched,
		rowsKey: r.rowsKey(),
	}
	for _, table := range r.tables {
		step.Before = append(step.Before, sortedRows(r.saved[table])...)
		step.After = append(step.After, sortedRows(r.after[table])...)
	}
	return step
}

func sortedRows(rows map[int64]SavedRow) []SavedRow {
	ids := make([]int64, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	ordered := make([]SavedRow, 0, len(ids))
	for _, id := range ids {
		ordered = append(ordered, rows[id])
	}
	return ordered
}

// loadRows reads full copies of the given rows.
func loadRows(tx *sql.Tx, table string, ids []int64) (map[int64]SavedRow, error) {
	copies := make(map[int64]SavedRow, len(ids))
	if len(ids) == 0 {
		return copies, nil
	}

	placeholders := ""
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}

	rows, err := tx.Query(fmt.Sprintf("SELECT * FROM %s WHERE id IN (%s)", table, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	idColumn := -1
	for i, column := range columns {
		if column == "id" {
			idColumn = i
			break
		}
	}
	if idColumn == -1 {
		return nil, fmt.Errorf("table %s has no id column", table)
	}

	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		id, ok := values[idColumn].(int64)
		if !ok {
			return nil, fmt.Errorf("table %s has a non-integer id column", table)
		}

		copiedColumns := make(map[string]any, len(columns))
		for i, column := range columns {
			copiedColumns[column] = values[i]
		}
		copies[id] = SavedRow{Table: table, ID: id, Columns: copiedColumns}
	}
	return copies, rows.Err()
}
