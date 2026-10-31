// Package store is the app's database actions: the table operations every
// command runs through. It is plain SQL — it knows nothing about commands
// or undo. The command engine wraps a Session to observe what happens.
package store

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Session is the database handle a command works with: raw queries for
// reads and special SQL, plus the three table operations. Insert, Delete
// and Update take plain values and ids — no SQL to write for common cases.
type Session interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)

	// Insert adds one row and returns its new id.
	Insert(table string, values map[string]any) (int64, error)
	// Delete removes rows by id. The database's own cascades remove their
	// children.
	Delete(table string, ids ...int64) error
	// Update writes the given columns onto one row.
	Update(table string, id int64, columns map[string]any) error
}

// Queryer is anything that can run SQL: *sql.DB or *sql.Tx.
type Queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// Wrap returns a Session over a transaction or the database itself.
func Wrap(q Queryer) Session {
	return &plainSession{q: q}
}

type plainSession struct {
	q Queryer
}

func (s *plainSession) Query(query string, args ...any) (*sql.Rows, error) {
	return s.q.Query(query, args...)
}

func (s *plainSession) QueryRow(query string, args ...any) *sql.Row {
	return s.q.QueryRow(query, args...)
}

func (s *plainSession) Exec(query string, args ...any) (sql.Result, error) {
	return s.q.Exec(query, args...)
}

func (s *plainSession) Insert(table string, values map[string]any) (int64, error) {
	columns := sortedKeys(values)
	placeholders := make([]string, len(columns))
	args := make([]any, len(columns))
	for i, column := range columns {
		placeholders[i] = "?"
		args[i] = values[column]
	}

	result, err := s.q.Exec(
		fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			table, strings.Join(columns, ", "), strings.Join(placeholders, ", ")),
		args...,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting into %s: %w", table, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting the new %s id: %w", table, err)
	}
	return id, nil
}

func (s *plainSession) Delete(table string, ids ...int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	if _, err := s.q.Exec(
		fmt.Sprintf("DELETE FROM %s WHERE id IN (%s)", table, placeholders), args...,
	); err != nil {
		return fmt.Errorf("deleting from %s: %w", table, err)
	}
	return nil
}

func (s *plainSession) Update(table string, id int64, columns map[string]any) error {
	if len(columns) == 0 {
		return nil
	}
	names := sortedKeys(columns)
	sets := make([]string, len(names))
	args := make([]any, 0, len(names)+1)
	for i, name := range names {
		sets[i] = name + " = ?"
		args = append(args, columns[name])
	}
	args = append(args, id)

	if _, err := s.q.Exec(
		fmt.Sprintf("UPDATE %s SET %s WHERE id = ?", table, strings.Join(sets, ", ")),
		args...,
	); err != nil {
		return fmt.Errorf("updating %s #%d: %w", table, id, err)
	}
	return nil
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
