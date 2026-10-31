// Package store is the app's database actions: the table operations every
// command runs through. It is plain SQL — it knows nothing about commands
// or undo. The command engine wraps a Session to record what happens.
package store

import (
	"database/sql"
	"fmt"

	"github.com/Masterminds/squirrel"
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
	query, args, err := squirrel.Insert(table).SetMap(values).ToSql()
	if err != nil {
		return 0, fmt.Errorf("building the insert: %w", err)
	}
	result, err := s.q.Exec(query, args...)
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
	query, args, err := squirrel.Delete(table).Where(squirrel.Eq{"id": ids}).ToSql()
	if err != nil {
		return fmt.Errorf("building the delete: %w", err)
	}
	if _, err := s.q.Exec(query, args...); err != nil {
		return fmt.Errorf("deleting from %s: %w", table, err)
	}
	return nil
}

func (s *plainSession) Update(table string, id int64, columns map[string]any) error {
	if len(columns) == 0 {
		return nil
	}
	query, args, err := squirrel.Update(table).SetMap(columns).Where(squirrel.Eq{"id": id}).ToSql()
	if err != nil {
		return fmt.Errorf("building the update: %w", err)
	}
	if _, err := s.q.Exec(query, args...); err != nil {
		return fmt.Errorf("updating %s #%d: %w", table, id, err)
	}
	return nil
}
