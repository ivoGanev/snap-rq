package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"snap-rq/backend/commands"
)

// maxOutputBytes caps one tool result. Response bodies are stored whole and
// can be huge; the caller should narrow the query instead of flooding the
// model's context.
const maxOutputBytes = 200_000

// Runner is the part of commands.Runner the MCP server uses: the same door
// the UI goes through.
type Runner interface {
	Run(name string, input json.RawMessage) (any, error)
	ListCommands() []commands.CommandInfo
}

// binding connects a catalog tool to its typed input. Add registers the tool
// on the SDK server.
type binding struct {
	Name string
	Add  func(s *Server, m ToolMeta)
}

// Server is the MCP server: a loopback HTTP endpoint in front of the Runner.
type Server struct {
	runner  Runner
	store   *Store
	policy  *Policy
	version string

	mu         sync.Mutex
	sdk        *sdk.Server
	httpSrv    *http.Server
	addr       string
	registered map[string]bool

	tokenMu sync.RWMutex
	token   string

	maskMu          sync.RWMutex
	requireApproval bool // risky tools ask first
	approver        Approver
	broker          *Broker
	masker          *Masker // nil means masking is off
	// auditMasker always masks: the audit log never stores secrets, even when
	// output masking is switched off.
	auditMasker *Masker
}

// NewServer returns a stopped server.
func NewServer(runner Runner, store *Store, version string) *Server {
	broker := NewBroker()
	return &Server{
		runner:          runner,
		store:           store,
		policy:          NewPolicy(nil),
		version:         version,
		registered:      map[string]bool{},
		masker:          NewMasker(nil), // on until the stored setting says otherwise
		auditMasker:     NewMasker(nil),
		requireApproval: true, // asking is the default until the stored setting says otherwise
		broker:          broker,
		approver:        broker,
	}
}

// StartIfEnabled starts the server when the stored setting says so, or when
// SNAPRQ_MCP=1 is set (a dev bootstrap that stores nothing). It never fails
// the app: problems are logged.
func (s *Server) StartIfEnabled() {
	devBoot := os.Getenv("SNAPRQ_MCP") == "1"
	if !s.store.Load().Enabled && !devBoot {
		return
	}
	addr, err := s.Start()
	if err != nil {
		log.Printf("mcp: not started: %v", err)
		return
	}
	if devBoot {
		log.Printf("mcp: listening on http://%s/mcp (dev bootstrap; the token is under Settings > MCP server)", addr)
	} else {
		log.Printf("mcp: listening on http://%s/mcp", addr)
	}
}

// Start begins serving on 127.0.0.1 and returns the listen address.
func (s *Server) Start() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.httpSrv != nil {
		return "", errors.New("already running")
	}

	var commandNames []string
	for _, c := range s.runner.ListCommands() {
		commandNames = append(commandNames, c.Name)
	}
	if err := ValidateCatalog(commandNames); err != nil {
		return "", err
	}
	if err := validateBindings(); err != nil {
		return "", err
	}

	cfg := s.store.Load()
	token, err := s.store.EnsureToken()
	if err != nil {
		return "", fmt.Errorf("preparing token: %w", err)
	}
	s.setToken(token)
	s.policy.Set(cfg.Overrides)
	s.applyConfig(cfg)

	s.sdk = sdk.NewServer(&sdk.Implementation{Name: "snap-rq", Version: s.version}, nil)
	s.registered = map[string]bool{}
	s.syncToolsLocked()

	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)))
	if err != nil {
		return "", fmt.Errorf("listening on port %d: %w", cfg.Port, err)
	}
	server := s.sdk
	// Stateless: every request stands alone, so a client that was connected
	// before SnapRQ restarted keeps working instead of presenting a session id
	// the new process has never seen. The tools are plain request/response.
	handler := sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return server },
		&sdk.StreamableHTTPOptions{Stateless: true},
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", Guard(s.currentToken, rememberUserAgent(handler)))
	s.httpSrv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func(srv *http.Server) {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("mcp: server stopped: %v", err)
		}
	}(s.httpSrv)
	s.addr = listener.Addr().String()
	return s.addr, nil
}

// Running reports whether the server is listening.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.httpSrv != nil
}

// Addr returns the listen address, or "" when stopped.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Stop shuts the server down; it is safe to call when not running.
func (s *Server) Stop() {
	s.mu.Lock()
	srv := s.httpSrv
	s.httpSrv = nil
	s.addr = ""
	s.mu.Unlock()
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// Reload re-reads the overrides and the token after a settings change: the
// tool list follows the policy and a rotated token takes effect at once.
func (s *Server) Reload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.httpSrv == nil {
		return
	}
	cfg := s.store.Load()
	s.policy.Set(cfg.Overrides)
	s.applyConfig(cfg)
	if token := s.store.Token(); token != "" {
		s.setToken(token)
	}
	s.syncToolsLocked()
}

// syncToolsLocked makes the SDK server's tool list match the policy. The
// policy is also checked on every call, so a tool switched off is refused
// even by a client that has not re-listed.
func (s *Server) syncToolsLocked() {
	for _, b := range bindings {
		meta, _ := metaFor(b.Name)
		allowed := s.policy.Allowed(meta)
		switch {
		case allowed && !s.registered[b.Name]:
			b.Add(s, meta)
			s.registered[b.Name] = true
		case !allowed && s.registered[b.Name]:
			s.sdk.RemoveTools(b.Name)
			delete(s.registered, b.Name)
		}
	}
}

func (s *Server) currentToken() string {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	return s.token
}

func (s *Server) setToken(token string) {
	s.tokenMu.Lock()
	s.token = token
	s.tokenMu.Unlock()
}

// validateBindings checks that every catalog tool has a binding and the
// other way round.
func validateBindings() error {
	bound := map[string]bool{}
	for _, b := range bindings {
		if _, ok := metaFor(b.Name); !ok {
			return fmt.Errorf("mcp binding %q has no catalog entry", b.Name)
		}
		bound[b.Name] = true
	}
	for _, m := range Catalog {
		if !bound[m.Name] {
			return fmt.Errorf("mcp tool %q has no binding", m.Name)
		}
	}
	return nil
}

// callFunc is a tool's behaviour: it receives the typed arguments.
type callFunc[In any] func(s *Server, in In) (any, error)

// addTool registers a tool whose arguments are passed to its command as-is:
// In's JSON field names are the command's input names.
func addTool[In any](s *Server, m ToolMeta) {
	register[In](s, m, func(s *Server, in In) (any, error) { return s.run(m.Command, in) })
}

// customTool returns a binding for a tool that does more than forward its
// arguments (defaults, or several commands in a row).
func customTool[In any](call callFunc[In]) func(*Server, ToolMeta) {
	return func(s *Server, m ToolMeta) { register[In](s, m, call) }
}

// register adds the tool to the SDK server. The policy is re-checked on
// every call, so a tool switched off is refused even by a client that has not
// re-listed.
func register[In any](s *Server, m ToolMeta, call callFunc[In]) {
	sdk.AddTool(s.sdk, &sdk.Tool{
		Name:        m.Name,
		Description: m.Description,
		Annotations: annotationsFor(m),
	}, func(ctx context.Context, req *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
		started := time.Now()
		client := clientName(ctx, req)
		fail := func(status string, err error) (*sdk.CallToolResult, any, error) {
			s.recordAudit(m, client, in, status, err, started)
			return nil, nil, err
		}
		if !s.policy.Allowed(m) {
			return fail(auditRefused, ErrDisabled)
		}
		if err := s.checkInput(in); err != nil {
			return fail(auditRefused, err)
		}
		if err := s.gate(ctx, m, client, in); err != nil {
			return fail(auditRefused, err)
		}
		out, err := call(s, in)
		if err != nil {
			return fail(auditError, err)
		}
		out, err = s.present(out)
		if err != nil {
			return fail(auditError, err)
		}
		text, err := cappedJSON(out)
		if err != nil {
			return fail(auditError, err)
		}
		s.recordAudit(m, client, in, auditOK, nil, started)
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}, nil, nil
	})
}

// run marshals the input and runs a command through the runner, the same
// door the UI uses.
func (s *Server) run(command string, in any) (any, error) {
	payload, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	return s.runner.Run(command, payload)
}

// runMap runs a command and returns its result as a generic JSON object,
// for tools that read a row, change a field and write it back.
func (s *Server) runMap(command string, in any) (map[string]any, error) {
	out, err := s.run(command, in)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber() // keep ids exact
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("%s did not return an object: %w", command, err)
	}
	return result, nil
}

// cappedJSON marshals a command result, refusing results over the cap.
func cappedJSON(out any) (string, error) {
	raw, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	if len(raw) > maxOutputBytes {
		return "", fmt.Errorf("result is too large (%d bytes, limit %d); narrow the query", len(raw), maxOutputBytes)
	}
	return string(raw), nil
}

// trashingTools are organize tools that remove things from view (to the
// trash); clients are told so they can prompt.
var trashingTools = map[string]bool{"delete_requests": true}

// annotationsFor tells clients what a tool does, so they can prompt
// accordingly. Destructive and open-world hints for the restricted classes
// arrive with those classes.
func annotationsFor(m ToolMeta) *sdk.ToolAnnotations {
	if m.Risk == RiskRead {
		return &sdk.ToolAnnotations{ReadOnlyHint: true}
	}
	destructive := trashingTools[m.Name] || m.Risk == RiskContent
	return &sdk.ToolAnnotations{DestructiveHint: &destructive}
}

func (s *Server) applyConfig(cfg Config) {
	s.maskMu.Lock()
	defer s.maskMu.Unlock()
	s.auditMasker = NewMasker(cfg.MaskExtra)
	s.requireApproval = cfg.RequireApproval
	if cfg.MaskSecrets {
		s.masker = NewMasker(cfg.MaskExtra)
	} else {
		s.masker = nil
	}
}

// SetEmitter sets how approval requests reach the window.
func (s *Server) SetEmitter(emit func(name string, data any)) { s.broker.SetEmit(emit) }

func (s *Server) currentAuditMasker() *Masker {
	s.maskMu.RLock()
	defer s.maskMu.RUnlock()
	if s.auditMasker == nil {
		return NewMasker(nil)
	}
	return s.auditMasker
}

func (s *Server) currentMasker() *Masker {
	s.maskMu.RLock()
	defer s.maskMu.RUnlock()
	return s.masker
}

// present is the output boundary: what a client receives passes through here.
// Tools themselves read real data, so write-backs are never masked.
func (s *Server) present(out any) (any, error) {
	m := s.currentMasker()
	if m == nil {
		return out, nil
	}
	generic, err := toGeneric(out)
	if err != nil {
		return nil, err
	}
	return m.Tree(generic), nil
}

// checkInput refuses arguments containing a masked value: a client that read
// "[REDACTED]" must not write it back over the real secret.
func (s *Server) checkInput(in any) error {
	if s.currentMasker() == nil {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if bytes.Contains(raw, []byte(Redacted)) {
		return errors.New("the input contains " + Redacted + ", a masked secret; use a {{variable}} placeholder or leave that field out")
	}
	return nil
}

// clientName is the connected client's name and version from the MCP
// handshake, or "" when it is not known.
func clientName(ctx context.Context, req *sdk.CallToolRequest) string {
	if req != nil && req.Session != nil {
		if params := req.Session.InitializeParams(); params != nil && params.ClientInfo != nil {
			if name := strings.TrimSpace(params.ClientInfo.Name + " " + params.ClientInfo.Version); name != "" {
				return name
			}
		}
	}
	// Stateless requests carry no handshake, so fall back to the HTTP client.
	ua, _ := ctx.Value(userAgentKey{}).(string)
	return truncate(strings.TrimSpace(ua), 80)
}

type userAgentKey struct{}

// rememberUserAgent keeps the caller's User-Agent for the audit log and the
// approval dialog.
func rememberUserAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), userAgentKey{}, r.UserAgent())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// recordAudit stores one call. A failure to log never fails the call.
func (s *Server) recordAudit(m ToolMeta, client string, in any, status string, callErr error, started time.Time) {
	if s.store == nil {
		return
	}
	s.maskMu.RLock()
	masker := s.auditMasker
	s.maskMu.RUnlock()
	entry := AuditEntry{
		At:         started.UnixMilli(),
		Tool:       m.Name,
		Client:     client,
		Status:     status,
		Summary:    summarize(in),
		Args:       maskArgs(masker, in),
		DurationMs: time.Since(started).Milliseconds(),
	}
	if callErr != nil {
		entry.Error = truncate(callErr.Error(), 500)
	}
	if err := s.store.AddAudit(entry); err != nil {
		log.Printf("mcp: audit log: %v", err)
	}
}
