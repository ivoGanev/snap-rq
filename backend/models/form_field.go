package models

import (
	"encoding/json"
	"strings"
)

// FormField is one row of a form-data body, as stored in HttpRequest.BodyForm.
type FormField struct {
	Key      string `json:"key"`
	Type     string `json:"type"` // "text" | "file"
	Value    string `json:"value"`
	FilePath string `json:"file_path"`
	FileName string `json:"file_name"`
	Enabled  bool   `json:"enabled"`
}

// ParseFormFields reads a stored form-data body; an empty one has no fields.
func ParseFormFields(raw string) ([]FormField, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var fields []FormField
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, err
	}
	return fields, nil
}
