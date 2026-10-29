// Package commands implements the command bus: the single choke point for
// workspace mutations, with memento-based undo/redo. See
// specs/command-framework.md for the design contract.
package commands

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"
)

// Category classifies how the bus treats a command's writes.
type Category int

const (
	// Undoable commands record a memento and land on the undo stack.
	Undoable Category = iota
	// SideEffect commands have external side effects (sending requests).
	// They execute in a transaction but never touch the stacks.
	SideEffect
	// System commands perform append-only/system writes (response history,
	// usage tracking, settings). Like SideEffect they never touch stacks.
	System
)

func (c Category) String() string {
	switch c {
	case Undoable:
		return "undoable"
	case SideEffect:
		return "side-effect"
	case System:
		return "system"
	}
	return "unknown"
}

// Change describes which entities a command touched. It drives change events
// (IDs only, never row values, so secrets never ride the event bus) and the
// undo result's UI-state refs.
type Change struct {
	Entity string  `json:"entity"`
	Op     string  `json:"op"`
	IDs    []int64 `json:"ids"`
}

// Command is one named mutation registered on the Bus. The name is public
// vocabulary: future external callers (AI/CLI) execute these exact names.
type Command struct {
	Name     string
	Label    string
	Category Category
	// CoalesceKey derives the merge key from the payload; "" = never merge.
	CoalesceKey func(payload json.RawMessage) string
	// Handler performs the mutation using ctx.Tx.
	Handler func(ctx *Context, payload json.RawMessage) (any, error)
}

// Row is a full column image of one row, keyed by column name.
type Row struct {
	ID     int64          `json:"id"`
	Values map[string]any `json:"values"`
}

// Memento holds before/after images for every captured row. A row present
// only in Before was deleted by the command; only in After, created; in
// both, updated (restore writes back only differing columns so interleaved
// writes to other columns are never clobbered).
//
// Tables preserves capture order: handlers snapshot parent tables first so
// restores insert parents before children.
type Memento struct {
	Tables []string                 `json:"tables"`
	Before map[string]map[int64]Row `json:"before"`
	After  map[string]map[int64]Row `json:"after"`
}

// Entry is one position on an undo/redo stack.
type Entry struct {
	Command     string
	Label       string
	At          time.Time
	CoalesceKey string
	Memento     *Memento
	Changes     []Change
}

func (e *Entry) refs() map[string][]int64 {
	refs := make(map[string][]int64, len(e.Changes))
	for _, c := range e.Changes {
		refs[c.Entity] = append(refs[c.Entity], c.IDs...)
	}
	return refs
}

var tableNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Context carries the transaction and capture helpers into a handler.
type Context struct {
	Tx *sql.Tx

	memento *Memento
	// afterIDs tracks every (table, id) to re-read after the handler runs;
	// Snapshot adds before-images too, CaptureNew only after-images.
	afterIDs map[string]map[int64]bool
	changes  []Change
}

func newContext(tx *sql.Tx) *Context {
	return &Context{
		Tx: tx,
		memento: &Memento{
			Before: map[string]map[int64]Row{},
			After:  map[string]map[int64]Row{},
		},
		afterIDs: map[string]map[int64]bool{},
	}
}

func (c *Context) table(name string) error {
	if !tableNamePattern.MatchString(name) {
		return fmt.Errorf("invalid table name %q", name)
	}
	if _, ok := c.memento.Before[name]; !ok {
		c.memento.Before[name] = map[int64]Row{}
		c.memento.After[name] = map[int64]Row{}
		c.memento.Tables = append(c.memento.Tables, name)
	}
	if c.afterIDs[name] == nil {
		c.afterIDs[name] = map[int64]bool{}
	}
	return nil
}

// Snapshot records rows the handler is about to change or delete, capturing
// their before-image inside the transaction. Snapshot parent tables before
// child tables: the capture order becomes the restore order. A row that does
// not exist produces no before-image; combined with CaptureNew this
// distinguishes updates, deletes and creates at apply time.
func (c *Context) Snapshot(table string, ids ...int64) error {
	if len(ids) == 0 {
		return nil
	}
	if err := c.table(table); err != nil {
		return err
	}

	rows, err := readRows(c.Tx, table, ids)
	if err != nil {
		return fmt.Errorf("snapshotting %s: %w", table, err)
	}
	for id, row := range rows {
		if _, captured := c.memento.Before[table][id]; !captured {
			c.memento.Before[table][id] = row
		}
	}
	for _, id := range ids {
		c.afterIDs[table][id] = true
	}
	return nil
}

// CaptureNew registers rows the handler has just created: only their
// after-image is captured, so undo deletes them.
func (c *Context) CaptureNew(table string, ids ...int64) error {
	if len(ids) == 0 {
		return nil
	}
	if err := c.table(table); err != nil {
		return err
	}
	for _, id := range ids {
		c.afterIDs[table][id] = true
	}
	return nil
}

// Affected declares which entities the command touched, for change events
// and the undo result's UI-state refs.
func (c *Context) Affected(entity, op string, ids ...int64) {
	if len(ids) == 0 {
		return
	}
	c.changes = append(c.changes, Change{Entity: entity, Op: op, IDs: ids})
}

// captureAfterImages re-reads every captured row inside the same
// transaction, so the after-image matches exactly what commits.
func (c *Context) captureAfterImages() error {
	for _, table := range c.memento.Tables {
		ids := make([]int64, 0, len(c.afterIDs[table]))
		for id := range c.afterIDs[table] {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

		rows, err := readRows(c.Tx, table, ids)
		if err != nil {
			return fmt.Errorf("capturing after-image for %s: %w", table, err)
		}
		c.memento.After[table] = rows
	}
	return nil
}

// readRows selects full column images for the given ids.
func readRows(q queryer, table string, ids []int64) (map[int64]Row, error) {
	result := make(map[int64]Row, len(ids))
	if len(ids) == 0 {
		return result, nil
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

	rows, err := q.Query(fmt.Sprintf("SELECT * FROM %s WHERE id IN (%s)", table, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	idIndex := -1
	for i, col := range cols {
		if col == "id" {
			idIndex = i
			break
		}
	}
	if idIndex == -1 {
		return nil, fmt.Errorf("table %s has no id column", table)
	}

	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		id, ok := values[idIndex].(int64)
		if !ok {
			return nil, fmt.Errorf("table %s id column is not an integer", table)
		}
		image := make(map[string]any, len(cols))
		for i, col := range cols {
			image[col] = values[i]
		}
		result[id] = Row{ID: id, Values: image}
	}
	return result, rows.Err()
}

type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}
