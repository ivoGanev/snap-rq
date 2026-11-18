package mcp

import (
	"fmt"
)

// SettingsService is the Wails door for the MCP settings UI. It owns no state:
// everything lives in the Store (app_settings) and the Server.
type SettingsService struct {
	server *Server
	store  *Store
}

// NewSettingsService wires the settings UI to the server and its store.
func NewSettingsService(server *Server, store *Store) *SettingsService {
	return &SettingsService{server: server, store: store}
}

// Status is the server state shown in the settings.
type Status struct {
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	URL     string `json:"url"`
	Error   string `json:"error"`
}

// ToolStatus is one tool row in the settings.
type ToolStatus struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Risk        string `json:"risk"`
	Enabled     bool   `json:"enabled"`
	Overridden  bool   `json:"overridden"`
}

func (s *SettingsService) status(startErr error) Status {
	cfg := s.store.Load()
	st := Status{Enabled: cfg.Enabled, Running: s.server.Running(), Port: cfg.Port}
	if st.Running {
		st.URL = "http://" + s.server.Addr() + "/mcp"
	}
	if startErr != nil {
		st.Error = startErr.Error()
	}
	return st
}

// Status returns the current state.
func (s *SettingsService) Status() Status { return s.status(nil) }

// SetEnabled turns the server on or off and remembers the choice. When the
// server cannot start (port in use, say) the choice is kept and the error is
// reported in the returned status.
func (s *SettingsService) SetEnabled(enabled bool) (Status, error) {
	if err := s.store.SetEnabled(enabled); err != nil {
		return Status{}, err
	}
	if !enabled {
		s.server.Stop()
		return s.status(nil), nil
	}
	if s.server.Running() {
		return s.status(nil), nil
	}
	_, err := s.server.Start()
	return s.status(err), nil
}

// SetPort stores a new port and restarts a running server on it.
func (s *SettingsService) SetPort(port int) (Status, error) {
	if err := s.store.SetPort(port); err != nil {
		return Status{}, err
	}
	if !s.server.Running() {
		return s.status(nil), nil
	}
	s.server.Stop()
	_, err := s.server.Start()
	return s.status(err), nil
}

// Token returns the bearer token, creating it on first use.
func (s *SettingsService) Token() (string, error) { return s.store.EnsureToken() }

// RotateToken replaces the token; connected clients stop working until they
// are given the new one.
func (s *SettingsService) RotateToken() (string, error) {
	token, err := s.store.RotateToken()
	if err != nil {
		return "", err
	}
	s.server.Reload()
	return token, nil
}

// Tools lists every tool with its effective availability. Classes that ship
// off by default and have no tools yet simply do not appear.
func (s *SettingsService) Tools() []ToolStatus {
	overrides := s.store.Load().Overrides
	policy := NewPolicy(overrides)
	out := make([]ToolStatus, 0, len(Catalog))
	for _, m := range Catalog {
		_, overridden := overrides[m.Name]
		out = append(out, ToolStatus{
			Name:        m.Name,
			Description: m.Description,
			Risk:        string(m.Risk),
			Enabled:     policy.Allowed(m),
			Overridden:  overridden,
		})
	}
	return out
}

// SetToolEnabled switches one tool on or off.
func (s *SettingsService) SetToolEnabled(name string, enabled bool) error {
	if err := s.store.SetToolOverride(name, enabled); err != nil {
		return err
	}
	s.server.Reload()
	return nil
}

// ResetTool returns one tool to its class default.
func (s *SettingsService) ResetTool(name string) error {
	if _, ok := metaFor(name); !ok {
		return fmt.Errorf("unknown tool %q", name)
	}
	if err := s.store.ClearToolOverride(name); err != nil {
		return err
	}
	s.server.Reload()
	return nil
}

// SetRiskEnabled switches every tool of one class on or off.
func (s *SettingsService) SetRiskEnabled(risk string, enabled bool) error {
	r := Risk(risk)
	if !r.valid() {
		return fmt.Errorf("unknown risk class %q", risk)
	}
	for _, m := range Catalog {
		if m.Risk != r {
			continue
		}
		if err := s.store.SetToolOverride(m.Name, enabled); err != nil {
			return err
		}
	}
	s.server.Reload()
	return nil
}

// Masking is the secret-masking setting shown in the settings.
type Masking struct {
	Enabled bool     `json:"enabled"`
	Extra   []string `json:"extra"`
	// Builtin are the names always hidden, so the UI can list them.
	Builtin []string `json:"builtin"`
}

// Masking returns the masking setting.
func (s *SettingsService) Masking() Masking {
	cfg := s.store.Load()
	extra := cfg.MaskExtra
	if extra == nil {
		extra = []string{}
	}
	return Masking{Enabled: cfg.MaskSecrets, Extra: extra, Builtin: BuiltinSecretNames()}
}

// SetMasking stores the masking setting and applies it to the running server.
func (s *SettingsService) SetMasking(enabled bool, extra []string) (Masking, error) {
	if err := s.store.SetMaskExtra(extra); err != nil {
		return Masking{}, err
	}
	if err := s.store.SetMaskSecrets(enabled); err != nil {
		return Masking{}, err
	}
	s.server.Reload()
	return s.Masking(), nil
}

// RecentActions returns the newest tool calls by AI clients, newest first.
func (s *SettingsService) RecentActions(limit int) ([]AuditEntry, error) {
	return s.store.RecentAudit(limit)
}

// ClearActions deletes the log of tool calls.
func (s *SettingsService) ClearActions() error { return s.store.ClearAudit() }
