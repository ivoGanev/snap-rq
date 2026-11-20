package models

// ScriptAppearance is how a script looks in the sidebar.
// A script always has exactly one appearance row: either an icon or a color.
type ScriptAppearance struct {
	ID              int64  `json:"id"`
	ScriptID        int64  `json:"script_id"`
	AppearanceType  string `json:"appearance_type"`
	AppearanceValue string `json:"appearance_value"`
}

// DefaultScriptAppearance returns the default appearance for a new script.
func DefaultScriptAppearance() ScriptAppearance {
	return ScriptAppearance{
		AppearanceType:  "icon",
		AppearanceValue: "ico_1a9d4",
	}
}
