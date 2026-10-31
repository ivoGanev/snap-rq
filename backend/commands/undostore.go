package commands

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultUndoLimit bounds the undo list when no settings function is
	// installed. The user can change it in settings.
	DefaultUndoLimit = 100

	// mergeWindow is how long quick repeats of the same command on the
	// same rows merge into one step.
	mergeWindow = 750 * time.Millisecond
)

// UndoStore holds the undo and redo lists. It is the only owner of undo
// data; it never writes the app's tables. Steps are plain JSON-friendly
// data, so persisting them into a table later is a contained change.
type UndoStore struct {
	undoSteps []*Step
	redoSteps []*Step

	// undoLimit is read through a function so the user can change it in
	// settings while the app runs.
	undoLimit func() int
}

// SetUndoLimit installs the settings-backed undo list size.
func (u *UndoStore) SetUndoLimit(fn func() int) {
	u.undoLimit = fn
}

func (u *UndoStore) limit() int {
	if u.undoLimit != nil {
		if n := u.undoLimit(); n > 0 {
			return n
		}
	}
	return DefaultUndoLimit
}

// Record adds a finished command's step to the undo list. Quick repeats of
// the same command on the same rows merge into the previous step — typing
// a new name is many runs of request.update on one row, and that is one
// step, not twenty.
func (u *UndoStore) Record(step *Step) {
	if len(u.undoSteps) > 0 {
		last := u.undoSteps[len(u.undoSteps)-1]
		if last.FunctionID == step.FunctionID && last.rowsKey == step.rowsKey &&
			time.Since(last.When) < mergeWindow {
			// Keep the oldest captures: one undo goes back to where the
			// typing started, not to the middle of it.
			last.When = time.Now()
			return
		}
	}

	// A new change makes redo impossible: it happens in a world the redo
	// steps know nothing about.
	u.redoSteps = nil
	u.undoSteps = append(u.undoSteps, step)

	for len(u.undoSteps) > u.limit() {
		u.undoSteps = u.undoSteps[1:]
	}
}

// PopUndo removes and returns the newest undo step.
func (u *UndoStore) PopUndo() *Step {
	if len(u.undoSteps) == 0 {
		return nil
	}
	step := u.undoSteps[len(u.undoSteps)-1]
	u.undoSteps = u.undoSteps[:len(u.undoSteps)-1]
	return step
}

// PushBackUndo returns a step to the undo list after a failed restore.
func (u *UndoStore) PushBackUndo(step *Step) {
	u.undoSteps = append(u.undoSteps, step)
}

// PushRedo moves an undone step onto the redo list.
func (u *UndoStore) PushRedo(step *Step) {
	u.redoSteps = append(u.redoSteps, step)
}

// PopRedo removes and returns the newest redo step.
func (u *UndoStore) PopRedo() *Step {
	if len(u.redoSteps) == 0 {
		return nil
	}
	step := u.redoSteps[len(u.redoSteps)-1]
	u.redoSteps = u.redoSteps[:len(u.redoSteps)-1]
	return step
}

// PushBackRedo returns a step to the redo list after a failed restore.
func (u *UndoStore) PushBackRedo(step *Step) {
	u.redoSteps = append(u.redoSteps, step)
}

// PushUndo moves a redone step back onto the undo list.
func (u *UndoStore) PushUndo(step *Step) {
	u.undoSteps = append(u.undoSteps, step)
}

// ForgetRows throws away every step that involves the given rows. Used
// when the user destroys data for good (emptying the trash): undo must
// never bring back something the user meant to destroy.
func (u *UndoStore) ForgetRows(table string, ids ...int64) {
	if len(ids) == 0 {
		return
	}
	involved := make(map[int64]bool, len(ids))
	for _, id := range ids {
		involved[id] = true
	}

	u.undoSteps = filterSteps(u.undoSteps, table, involved)
	u.redoSteps = filterSteps(u.redoSteps, table, involved)
}

func filterSteps(steps []*Step, table string, ids map[int64]bool) []*Step {
	kept := steps[:0]
	for _, step := range steps {
		if !stepInvolves(step, table, ids) {
			kept = append(kept, step)
		}
	}
	return kept
}

func stepInvolves(step *Step, table string, ids map[int64]bool) bool {
	for _, row := range step.Created {
		if row.Table == table && ids[rowID(row.Values)] {
			return true
		}
	}
	for _, set := range step.Deleted {
		if set.Table == table {
			for _, values := range set.Rows {
				if ids[rowID(values)] {
					return true
				}
			}
		}
	}
	for _, update := range step.Updated {
		if update.Table == table && ids[update.ID] {
			return true
		}
	}
	return false
}

func rowID(values map[string]any) int64 {
	id, _ := values["id"].(int64)
	return id
}

// Status is the summary the undo menu keeps on screen.
type Status struct {
	UndoSteps     int    `json:"undoSteps"`
	RedoSteps     int    `json:"redoSteps"`
	NextUndoLabel string `json:"nextUndoLabel"`
	NextRedoLabel string `json:"nextRedoLabel"`
	LastAction    string `json:"lastAction"` // "run", "undo" or "redo"
	LastLabel     string `json:"lastLabel"`
}

// Status returns the current summary.
func (u *UndoStore) Status(lastAction, lastLabel string) Status {
	status := Status{
		UndoSteps:  len(u.undoSteps),
		RedoSteps:  len(u.redoSteps),
		LastAction: lastAction,
		LastLabel:  lastLabel,
	}
	if len(u.undoSteps) > 0 {
		status.NextUndoLabel = u.undoSteps[len(u.undoSteps)-1].Label
	}
	if len(u.redoSteps) > 0 {
		status.NextRedoLabel = u.redoSteps[len(u.redoSteps)-1].Label
	}
	return status
}

// StepInfo describes one step for the inspector panel.
type StepInfo struct {
	Seq     int    `json:"seq"`
	Command string `json:"command"`
	Label   string `json:"label"`
	At      string `json:"at"`
	State   string `json:"state"` // "undo" or "redo"
	// Rows names the tables and row ids the step touched, e.g.
	// "http_requests: 1, 2 · responses: 5".
	Rows string `json:"rows"`
}

// Steps lists every remembered step, oldest first, for the panel.
func (u *UndoStore) Steps() []StepInfo {
	infos := make([]StepInfo, 0, len(u.undoSteps)+len(u.redoSteps))
	for i, step := range u.undoSteps {
		infos = append(infos, stepInfo(step, i, "undo"))
	}
	for i, step := range u.redoSteps {
		infos = append(infos, stepInfo(step, len(u.undoSteps)+i, "redo"))
	}
	return infos
}

func stepInfo(step *Step, seq int, state string) StepInfo {
	tables := []string{}
	ids := map[string][]int64{}
	add := func(table string, id int64) {
		if _, seen := ids[table]; !seen {
			tables = append(tables, table)
		}
		for _, existing := range ids[table] {
			if existing == id {
				return
			}
		}
		ids[table] = append(ids[table], id)
	}
	for _, row := range step.Created {
		add(row.Table, rowID(row.Values))
	}
	for _, set := range step.Deleted {
		for _, values := range set.Rows {
			add(set.Table, rowID(values))
		}
	}
	for _, update := range step.Updated {
		add(update.Table, update.ID)
	}

	parts := make([]string, 0, len(tables))
	for _, table := range tables {
		texts := make([]string, 0, len(ids[table]))
		for _, id := range ids[table] {
			texts = append(texts, strconv.FormatInt(id, 10))
		}
		parts = append(parts, table+": "+strings.Join(texts, ", "))
	}

	return StepInfo{
		Seq:     seq,
		Command: step.FunctionID,
		Label:   step.Label,
		At:      step.When.UTC().Format("2006-01-02 15:04:05"),
		State:   state,
		Rows:    strings.Join(parts, " · "),
	}
}

// rowsKey names the rows a step involved, e.g. "http_requests:42". Two
// runs of the same command with the same rowsKey are the same edit
// repeated.
func (s *Step) computeRowsKey() string {
	parts := []string{}
	add := func(table string, id int64) {
		parts = append(parts, table+":"+strconv.FormatInt(id, 10))
	}
	for _, row := range s.Created {
		add(row.Table, rowID(row.Values))
	}
	for _, set := range s.Deleted {
		for _, values := range set.Rows {
			add(set.Table, rowID(values))
		}
	}
	for _, update := range s.Updated {
		add(update.Table, update.ID)
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// UndoResult reports what an Undo or Redo did.
type UndoResult struct {
	Command string `json:"command"`
	Label   string `json:"label"`
	// Skipped lists steps that could not be undone because their rows were
	// destroyed for good (e.g. the trash was emptied permanently).
	Skipped []string `json:"skipped,omitempty"`
}
