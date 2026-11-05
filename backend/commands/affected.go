package commands

import "encoding/json"

// Affected names the rows a command touched, grouped by kind. A command
// that writes to two tables reports two entries (a collection delete takes
// its collection rows and its request rows).
type Affected struct {
	Component string  `json:"component"`
	IDs       []int64 `json:"ids"`
}

// AffectedCommand reports the rows a command wrote, so the frontend can
// refresh exactly those without guessing at input/result shapes.
type AffectedCommand interface {
	Affected(input, result json.RawMessage) []Affected
}

// IDsOf collects the numeric ids stored under the given fields. A field may
// hold a single number or an array of numbers, and a top-level array is
// searched object by object. Commands use it to keep Affected short.
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
