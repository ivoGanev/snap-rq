package commands

import "encoding/json"

// RowChange names the rows a command wrote, grouped by component. A
// command that writes to two tables reports two entries (a request delete
// touches request rows and trash rows).
type RowChange struct {
	Component string  `json:"component"`
	IDs       []int64 `json:"ids"`
}

// ReportsChanges is implemented by undoable commands: undo and redo run
// without a caller that could refresh the UI, so the command itself says
// which rows it changed.
type ReportsChanges interface {
	Changes(input, result json.RawMessage) []RowChange
}

// IDsOf collects the numeric ids stored under the given fields. A field may
// hold a single number or an array of numbers, and a top-level array is
// searched object by object. Commands use it to keep Changes short.
func IDsOf(raw json.RawMessage, fields ...string) []int64 {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	var ids []int64
	collectIDs(value, fields, &ids)
	return ids
}

func collectIDs(value any, fields []string, ids *[]int64) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			collectIDs(item, fields, ids)
		}
	case map[string]any:
		for _, field := range fields {
			switch f := v[field].(type) {
			case float64:
				*ids = append(*ids, int64(f))
			case []any:
				for _, item := range f {
					if n, ok := item.(float64); ok {
						*ids = append(*ids, int64(n))
					}
				}
			}
		}
	}
}
