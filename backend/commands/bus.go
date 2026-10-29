package commands

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	// DefaultUndoLimit bounds the undo stack when no settings provider is
	// configured.
	DefaultUndoLimit = 100

	// coalesceWindow is how long consecutive commands with the same
	// coalesce key merge into one entry (e.g. keystrokes on one rename).
	coalesceWindow = 750 * time.Millisecond

	// EventChanged is emitted after every committed command. Payload is
	// ChangedEvent; it carries entity IDs only, never row values.
	EventChanged = "command:changed"
	// EventHistory is emitted whenever stack depths change. Payload is
	// HistorySnapshot.
	EventHistory = "command:history"
)

// ErrNothingToUndo is returned by Undo on an empty stack.
var ErrNothingToUndo = errors.New("nothing to undo")

// ErrNothingToRedo is returned by Redo on an empty stack.
var ErrNothingToRedo = errors.New("nothing to redo")

// Bus executes commands, records mementos for undoable ones, and owns the
// undo/redo stacks. All mutations go through it; it serializes commands so
// SQLite's single-writer nature and stack ordering are respected.
type Bus struct {
	db       *sql.DB
	mu       sync.Mutex
	registry map[string]*Command
	undo     []*Entry
	redo     []*Entry

	limitProvider func() int
	lastAction    string
	lastLabel     string
}

func NewBus(db *sql.DB) *Bus {
	return &Bus{
		db:       db,
		registry: map[string]*Command{},
	}
}

// SetLimitProvider installs the undo history cap provider (wired to app
// settings). nil values or non-positive results fall back to
// DefaultUndoLimit.
func (b *Bus) SetLimitProvider(fn func() int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.limitProvider = fn
}

func (b *Bus) limit() int {
	if b.limitProvider != nil {
		if n := b.limitProvider(); n > 0 {
			return n
		}
	}
	return DefaultUndoLimit
}

// Register adds a command to the registry.
func (b *Bus) Register(cmd *Command) error {
	if cmd.Name == "" {
		return errors.New("command name is required")
	}
	if cmd.Handler == nil {
		return fmt.Errorf("command %s has no handler", cmd.Name)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.registry[cmd.Name]; exists {
		return fmt.Errorf("command %s is already registered", cmd.Name)
	}
	b.registry[cmd.Name] = cmd
	return nil
}

// Execute runs a named command. Undoable commands commit their mutation and
// memento atomically and land on the undo stack; SideEffect and System
// commands commit but never touch the stacks.
//
// Event emission happens after the mutex is released: Emit can block on a
// busy webview, and holding the bus lock across it would stall every queued
// command (a laggy undo).
func (b *Bus) Execute(name string, payload json.RawMessage) (any, error) {
	b.mu.Lock()
	result, emissions, err := b.executeLocked(name, payload)
	b.mu.Unlock()
	runEmissions(emissions)
	return result, err
}

func (b *Bus) executeLocked(name string, payload json.RawMessage) (any, []func(), error) {
	cmd, ok := b.registry[name]
	if !ok {
		return nil, nil, fmt.Errorf("unknown command %q", name)
	}

	tx, err := b.db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("beginning command transaction: %w", err)
	}
	defer tx.Rollback()

	ctx := newContext(tx)
	result, err := cmd.Handler(ctx, payload)
	if err != nil {
		return nil, nil, err
	}

	if cmd.Category == Undoable {
		if err := ctx.captureAfterImages(); err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, fmt.Errorf("committing command: %w", err)
		}
		b.pushLocked(cmd, payload, ctx)
		b.lastAction, b.lastLabel = "execute", cmd.Label
	} else {
		if err := tx.Commit(); err != nil {
			return nil, nil, fmt.Errorf("committing command: %w", err)
		}
	}

	emissions := []func(){b.changedEmission(name, ctx.changes), b.historyEmission()}
	return result, emissions, nil
}

// pushLocked records a new undo entry, coalescing when the previous entry
// shares the coalesce key and is recent, and trimming to the configured
// limit. A new undoable action always discards the redo stack.
func (b *Bus) pushLocked(cmd *Command, payload json.RawMessage, ctx *Context) {
	key := ""
	if cmd.CoalesceKey != nil {
		key = cmd.CoalesceKey(payload)
	}

	if key != "" && len(b.undo) > 0 {
		top := b.undo[len(b.undo)-1]
		if top.CoalesceKey == key && time.Since(top.At) < coalesceWindow {
			// Keep the oldest before-image and the newest after-image;
			// row-image mementos make the merge exactly the combined entry.
			top.Memento.After = ctx.memento.After
			top.At = time.Now()
			top.Changes = ctx.changes
			return
		}
	}

	b.redo = nil
	b.undo = append(b.undo, &Entry{
		Command:     cmd.Name,
		Label:       cmd.Label,
		At:          time.Now(),
		CoalesceKey: key,
		Memento:     ctx.memento,
		Changes:     ctx.changes,
	})

	limit := b.limit()
	for len(b.undo) > limit {
		b.undo = b.undo[1:]
	}
}

// UndoResult reports what an Undo/Redo did, including refs for UI-state
// restoration and notices for entries skipped because their targets were
// gone.
type UndoResult struct {
	Command string             `json:"command"`
	Label   string             `json:"label"`
	Refs    map[string][]int64 `json:"refs"`
	Notices []string           `json:"notices,omitempty"`
}

// Undo applies the newest undo entry's before-image. Stale entries (update
// targets gone) are skipped with a notice; a failed restore aborts its
// transaction, the entry returns to the stack, and the error surfaces.
func (b *Bus) Undo() (UndoResult, error) {
	b.mu.Lock()
	result, emissions, err := b.undoLocked()
	b.mu.Unlock()
	runEmissions(emissions)
	return result, err
}

func (b *Bus) undoLocked() (UndoResult, []func(), error) {
	var notices []string
	for len(b.undo) > 0 {
		entry := b.undo[len(b.undo)-1]
		b.undo = b.undo[:len(b.undo)-1]

		if err := b.validateEntry(entry); err != nil {
			notices = append(notices, fmt.Sprintf("Skipped %q: %v", entry.Label, err))
			continue
		}

		if err := b.apply(entry.Memento.Before, entry.Memento.After, entry.Memento.Tables); err != nil {
			b.undo = append(b.undo, entry)
			return UndoResult{Notices: notices}, nil, fmt.Errorf("undoing %s: %w", entry.Command, err)
		}

		b.redo = append(b.redo, entry)
		b.lastAction, b.lastLabel = "undo", entry.Label
		emissions := []func(){b.changedEmission(entry.Command, entry.Changes), b.historyEmission()}
		return UndoResult{
			Command: entry.Command,
			Label:   entry.Label,
			Refs:    entry.refs(),
			Notices: notices,
		}, emissions, nil
	}
	return UndoResult{Notices: notices}, nil, ErrNothingToUndo
}

// Redo re-applies the newest undone entry's after-image.
func (b *Bus) Redo() (UndoResult, error) {
	b.mu.Lock()
	result, emissions, err := b.redoLocked()
	b.mu.Unlock()
	runEmissions(emissions)
	return result, err
}

func (b *Bus) redoLocked() (UndoResult, []func(), error) {
	if len(b.redo) == 0 {
		return UndoResult{}, nil, ErrNothingToRedo
	}

	entry := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]

	if err := b.apply(entry.Memento.After, entry.Memento.Before, entry.Memento.Tables); err != nil {
		b.redo = append(b.redo, entry)
		return UndoResult{}, nil, fmt.Errorf("redoing %s: %w", entry.Command, err)
	}

	b.undo = append(b.undo, entry)
	b.lastAction, b.lastLabel = "redo", entry.Label
	emissions := []func(){b.changedEmission(entry.Command, entry.Changes), b.historyEmission()}
	return UndoResult{
		Command: entry.Command,
		Label:   entry.Label,
		Refs:    entry.refs(),
	}, emissions, nil
}

// HistorySnapshot is the payload of EventHistory and the History() result.
type HistorySnapshot struct {
	UndoDepth     int    `json:"undoDepth"`
	RedoDepth     int    `json:"redoDepth"`
	NextUndoLabel string `json:"nextUndoLabel"`
	NextRedoLabel string `json:"nextRedoLabel"`
	LastAction    string `json:"lastAction"`
	LastLabel     string `json:"lastLabel"`
}

// History returns the current stack summary for UI labels.
func (b *Bus) History() HistorySnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.historyLocked()
}

func (b *Bus) historyLocked() HistorySnapshot {
	snapshot := HistorySnapshot{
		UndoDepth:  len(b.undo),
		RedoDepth:  len(b.redo),
		LastAction: b.lastAction,
		LastLabel:  b.lastLabel,
	}
	if len(b.undo) > 0 {
		snapshot.NextUndoLabel = b.undo[len(b.undo)-1].Label
	}
	if len(b.redo) > 0 {
		snapshot.NextRedoLabel = b.redo[len(b.redo)-1].Label
	}
	return snapshot
}

// StackEntryInfo describes one stack entry for the development panel.
type StackEntryInfo struct {
	Seq     int                `json:"seq"`
	Command string             `json:"command"`
	Label   string             `json:"label"`
	At      string             `json:"at"`
	State   string             `json:"state"`
	Tables  map[string][]int64 `json:"tables"`
}

// Stack lists every live entry, oldest first, for the development panel.
func (b *Bus) Stack() []StackEntryInfo {
	b.mu.Lock()
	defer b.mu.Unlock()

	infos := make([]StackEntryInfo, 0, len(b.undo)+len(b.redo))
	for i, entry := range b.undo {
		infos = append(infos, stackInfo(entry, i, "undo"))
	}
	for i, entry := range b.redo {
		infos = append(infos, stackInfo(entry, len(b.undo)+i, "redo"))
	}
	return infos
}

func stackInfo(entry *Entry, seq int, state string) StackEntryInfo {
	tables := make(map[string][]int64, len(entry.Memento.Tables))
	for _, table := range entry.Memento.Tables {
		ids := make([]int64, 0, len(entry.Memento.Before[table])+len(entry.Memento.After[table]))
		for id := range entry.Memento.Before[table] {
			ids = append(ids, id)
		}
		for id := range entry.Memento.After[table] {
			if _, ok := entry.Memento.Before[table][id]; !ok {
				ids = append(ids, id)
			}
		}
		tables[table] = ids
	}
	return StackEntryInfo{
		Seq:     seq,
		Command: entry.Command,
		Label:   entry.Label,
		At:      entry.At.UTC().Format("2006-01-02 15:04:05"),
		State:   state,
		Tables:  tables,
	}
}

// CommandInfo describes a registered command for self-documenting callers.
type CommandInfo struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Category string `json:"category"`
}

// ListCommands returns the command registry for future external callers.
func (b *Bus) ListCommands() []CommandInfo {
	b.mu.Lock()
	defer b.mu.Unlock()

	infos := make([]CommandInfo, 0, len(b.registry))
	for _, cmd := range b.registry {
		infos = append(infos, CommandInfo{Name: cmd.Name, Label: cmd.Label, Category: cmd.Category.String()})
	}
	return infos
}

// Purge drops entries from both stacks whose mementos reference the given
// rows. Explicitly permanent operations (permanently emptying the bin) call
// this so undo can never resurrect destroyed data.
func (b *Bus) Purge(table string, ids ...int64) {
	if len(ids) == 0 {
		return
	}
	idSet := make(map[int64]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	keep := func(entries []*Entry) []*Entry {
		kept := entries[:0]
		for _, entry := range entries {
			refs := false
			for _, images := range []map[string]map[int64]Row{entry.Memento.Before, entry.Memento.After} {
				for id := range images[table] {
					if idSet[id] {
						refs = true
						break
					}
				}
				if refs {
					break
				}
			}
			if !refs {
				kept = append(kept, entry)
			}
		}
		return kept
	}
	b.undo = keep(b.undo)
	b.redo = keep(b.redo)
}

// ChangedEvent is the EventChanged payload.
type ChangedEvent struct {
	Command string   `json:"command"`
	Changes []Change `json:"changes"`
}

// changedEmission returns a closure that emits the command:changed event.
// Emissions run outside the bus mutex so a busy webview can never stall
// queued commands.
func (b *Bus) changedEmission(name string, changes []Change) func() {
	return func() {
		if app := application.Get(); app != nil {
			app.Event.Emit(EventChanged, ChangedEvent{Command: name, Changes: changes})
		}
	}
}

// historyEmission returns a closure that emits the command:history event.
func (b *Bus) historyEmission() func() {
	snapshot := b.historyLocked()
	return func() {
		if app := application.Get(); app != nil {
			app.Event.Emit(EventHistory, snapshot)
		}
	}
}

func runEmissions(emissions []func()) {
	for _, emit := range emissions {
		emit()
	}
}
