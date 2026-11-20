package models

// Script is code a user writes once per project and attaches to many
// requests. Scripts cannot run yet; for now only their text is stored.
type Script struct {
	ID         int64            `json:"id"`
	ProjectID  int64            `json:"project_id"`
	Name       string           `json:"name"`
	Content    string           `json:"content"`
	Appearance ScriptAppearance `json:"appearance"`
}
