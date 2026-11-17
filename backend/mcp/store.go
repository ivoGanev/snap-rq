package mcp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// The MCP settings live in app_settings next to the other preferences, as
// JSON-encoded values. The typed settings commands only touch their own keys,
// so the token never travels to the frontend through them.
const (
	keyEnabled   = "mcp_enabled"
	keyPort      = "mcp_port"
	keyToken     = "mcp_token"
	keyOverrides = "mcp_tool_overrides"
	keyMask      = "mcp_mask_secrets" // absent means on
	keyMaskExtra = "mcp_mask_extra"

	// DefaultPort is an arbitrary unprivileged port; the user can change it.
	DefaultPort = 47821
)

// Config is the stored MCP configuration. The token is deliberately not part
// of it: read it with Store.Token.
type Config struct {
	Enabled   bool
	Port      int
	Overrides map[string]bool

	// MaskSecrets hides secrets in tool output; on unless switched off.
	MaskSecrets bool
	// MaskExtra are the user's own sensitive names.
	MaskExtra []string
}

// Store reads and writes the MCP settings.
type Store struct {
	db *sql.DB
	mu sync.Mutex // serialises read-modify-write of the overrides
}

// NewStore returns a store on the app database.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) get(key string, dest any) bool {
	var raw string
	if err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&raw); err != nil {
		return false
	}
	return json.Unmarshal([]byte(raw), dest) == nil
}

func (s *Store) set(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO app_settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(raw))
	return err
}

// Load returns the stored configuration, with defaults for anything unset.
func (s *Store) Load() Config {
	cfg := Config{Port: DefaultPort, Overrides: map[string]bool{}, MaskSecrets: true}
	s.get(keyEnabled, &cfg.Enabled)
	var port int
	if s.get(keyPort, &port) && validPort(port) {
		cfg.Port = port
	}
	var overrides map[string]bool
	if s.get(keyOverrides, &overrides) && overrides != nil {
		cfg.Overrides = overrides
	}
	s.get(keyMask, &cfg.MaskSecrets)
	s.get(keyMaskExtra, &cfg.MaskExtra)
	return cfg
}

func validPort(p int) bool { return p >= 1024 && p <= 65535 }

// SetEnabled stores whether the server starts on launch.
func (s *Store) SetEnabled(enabled bool) error { return s.set(keyEnabled, enabled) }

// SetPort stores the loopback port.
func (s *Store) SetPort(port int) error {
	if !validPort(port) {
		return fmt.Errorf("port must be between 1024 and 65535")
	}
	return s.set(keyPort, port)
}

// SetToolOverride switches one tool on or off, regardless of its class.
func (s *Store) SetToolOverride(name string, allowed bool) error {
	if _, ok := metaFor(name); !ok {
		return fmt.Errorf("unknown tool %q", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	overrides := s.Load().Overrides
	overrides[name] = allowed
	return s.set(keyOverrides, overrides)
}

// ClearToolOverride returns a tool to its class default.
func (s *Store) ClearToolOverride(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	overrides := s.Load().Overrides
	delete(overrides, name)
	return s.set(keyOverrides, overrides)
}

// Token returns the bearer token, or "" when none has been generated yet.
func (s *Store) Token() string {
	var token string
	s.get(keyToken, &token)
	return token
}

// EnsureToken returns the token, generating and storing one on first use.
func (s *Store) EnsureToken() (string, error) {
	if token := s.Token(); token != "" {
		return token, nil
	}
	return s.RotateToken()
}

// RotateToken replaces the token; clients must be given the new one.
func (s *Store) RotateToken() (string, error) {
	token, err := GenerateToken()
	if err != nil {
		return "", err
	}
	if err := s.set(keyToken, token); err != nil {
		return "", err
	}
	return token, nil
}

// SetMaskSecrets switches secret masking of tool output on or off.
func (s *Store) SetMaskSecrets(on bool) error { return s.set(keyMask, on) }

// SetMaskExtra stores the user's own sensitive names: trimmed, lower-cased,
// de-duplicated, at most 100 of up to 64 characters.
func (s *Store) SetMaskExtra(names []string) error {
	clean := []string{}
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" || seen[n] {
			continue
		}
		if len(n) > 64 {
			return fmt.Errorf("name %q is longer than 64 characters", n[:20]+"…")
		}
		if len(clean) == 100 {
			return fmt.Errorf("at most 100 names")
		}
		seen[n] = true
		clean = append(clean, n)
	}
	return s.set(keyMaskExtra, clean)
}
