// Package scripting runs a request's scripts around its send: each attached
// script may define beforeRequest(req, ctx), which can change the request,
// and afterResponse(res, ctx), which can read the response and run tests.
// Scripts run in a sandbox: they get plain JavaScript and exactly the API set
// up here, no files, network or app commands. See docs/plans/scripting.md.
package scripting

import (
	"fmt"
	"time"

	"snap-rq/backend/models"
)

// The two phases of a send a script can hook into, and loading (its
// top-level code), which a run log shows when it prints or fails.
const (
	PhaseBefore = "beforeRequest"
	PhaseAfter  = "afterResponse"
	PhaseLoad   = "load"
)

// TimeLimit bounds each script's loading and each hook.
const TimeLimit = 5 * time.Second

// Boilerplate is a new script's code: both hooks, empty.
const Boilerplate = `// Runs before the request is sent: change the request here.
function beforeRequest(req, ctx) {
}

// Runs after the response arrives: test it here.
function afterResponse(res, ctx) {
}
`

// Script is one script attached to the request, in run order.
type Script struct {
	ID     int64
	Name   string
	Source string
}

// Request is the request as beforeRequest sees and changes it. Variables are
// not filled in yet: scripts see and may write {{placeholders}}.
type Request struct {
	Method  string
	URL     string
	Body    string
	Headers *Headers
}

// Response is what afterResponse reads.
type Response struct {
	Status     int
	Headers    *Headers
	Body       string
	DurationMs int64
}

// Env gives scripts read access to the selected environment's variables,
// secret ones included (decrypted by the backend; masked in the run log).
type Env interface {
	Get(key string) (string, bool)
}

// Engine runs one language's scripts. JavaScript is the only one so far;
// req, res and ctx mean the same in every language.
type Engine interface {
	// Load runs the script's top-level code and returns its hooks.
	Load(script Script, ctx *Context) (Program, error)
}

// Program is a loaded script.
type Program interface {
	Has(phase string) bool
	BeforeRequest(req *Request, ctx *Context) error
	AfterResponse(res *Response, ctx *Context) error
}

// Context is what a script can reach beyond req/res: the environment, values
// shared between the scripts of one send, tests, the console, and skipping
// the send. Each hook call gets one, recording into that call's run.
type Context struct {
	env     Env
	vars    map[string]any
	run     *models.ScriptRun
	skipped string
}

// EnvGet reads an environment variable.
func (c *Context) EnvGet(key string) (string, bool) {
	if c.env == nil {
		return "", false
	}
	return c.env.Get(key)
}

// Var reads a value an earlier script of this send stored.
func (c *Context) Var(key string) (any, bool) {
	v, ok := c.vars[key]
	return v, ok
}

// SetVar stores a value for the scripts that run after this one.
func (c *Context) SetVar(key string, value any) { c.vars[key] = value }

// Log records a console line.
func (c *Context) Log(level, text string) {
	c.run.Console = append(c.run.Console, models.ConsoleLine{Level: level, Text: text})
}

// Test records a test result.
func (c *Context) Test(name string, passed bool, failure string) {
	c.run.Tests = append(c.run.Tests, models.TestResult{Name: name, Passed: passed, Error: failure})
}

// Skip asks for the request not to be sent (beforeRequest only).
func (c *Context) Skip(reason string) {
	if reason == "" {
		reason = "no reason given"
	}
	c.skipped = reason
}

// Session runs one send's scripts: beforeRequest for each in order, then,
// once the response is in, afterResponse for each in the same order. Each
// script keeps its own sandbox for the whole send.
type Session struct {
	engine   Engine
	scripts  []Script
	programs []Program
	env      Env
	vars     map[string]any
	log      []models.ScriptRun
}

// NewSession prepares the scripts of one send, in run order.
func NewSession(scripts []Script, env Env) *Session {
	return &Session{
		engine:   javaScript{limit: TimeLimit},
		scripts:  scripts,
		programs: make([]Program, len(scripts)),
		env:      env,
		vars:     map[string]any{},
	}
}

// BeforeRequest loads each script and runs its beforeRequest, in order, each
// seeing the request as the previous one left it. An error stops the send: it
// names the script, and the request must not be sent. A script calling
// ctx.skip stops the send too, returning the reason.
func (s *Session) BeforeRequest(req *Request) (skipped string, err error) {
	for i, script := range s.scripts {
		run := models.ScriptRun{ScriptID: script.ID, Script: script.Name, Phase: PhaseBefore}
		ctx := s.context(&run)
		start := time.Now()

		program := s.programs[i]
		if program == nil {
			program, err = s.engine.Load(script, ctx)
		}
		if err == nil {
			s.programs[i] = program
			if program.Has(PhaseBefore) {
				err = program.BeforeRequest(req, ctx)
			}
		}
		run.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			run.Error = err.Error()
		}
		if hasOutput(run) {
			s.log = append(s.log, run)
		}
		if err != nil {
			return "", fmt.Errorf("script %q failed in beforeRequest: %s", script.Name, run.Error)
		}
		if ctx.skipped != "" {
			return fmt.Sprintf("script %q skipped the request: %s", script.Name, ctx.skipped), nil
		}
	}
	return "", nil
}

// Load loads every script without running a hook, e.g. to try afterResponse
// on its own. Output of a script's top-level code is logged as phase "load".
// An error names the script; nothing more can run then.
func (s *Session) Load() error {
	for i, script := range s.scripts {
		if s.programs[i] != nil {
			continue
		}
		run := models.ScriptRun{ScriptID: script.ID, Script: script.Name, Phase: PhaseLoad}
		start := time.Now()
		program, err := s.engine.Load(script, s.context(&run))
		run.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			run.Error = err.Error()
		}
		if hasOutput(run) {
			s.log = append(s.log, run)
		}
		if err != nil {
			return fmt.Errorf("script %q failed to load: %s", script.Name, run.Error)
		}
		s.programs[i] = program
	}
	return nil
}

// Defines reports whether any loaded script defines the phase's hook.
func (s *Session) Defines(phase string) bool {
	for _, program := range s.programs {
		if program != nil && program.Has(phase) {
			return true
		}
	}
	return false
}

// AfterResponse runs each script's afterResponse in order. An error counts
// against that script only (shown as its error); the others still run.
func (s *Session) AfterResponse(res *Response) {
	for i, program := range s.programs {
		if program == nil || !program.Has(PhaseAfter) {
			continue
		}
		script := s.scripts[i]
		run := models.ScriptRun{ScriptID: script.ID, Script: script.Name, Phase: PhaseAfter}
		ctx := s.context(&run)
		start := time.Now()
		if err := program.AfterResponse(res, ctx); err != nil {
			run.Error = err.Error()
		}
		run.DurationMs = time.Since(start).Milliseconds()
		if hasOutput(run) {
			s.log = append(s.log, run)
		}
	}
}

// Log returns what the scripts did, with mask applied to every text (to
// hide secret values a script printed).
func (s *Session) Log(mask func(string) string) []models.ScriptRun {
	for i := range s.log {
		run := &s.log[i]
		run.Error = mask(run.Error)
		for j := range run.Console {
			run.Console[j].Text = mask(run.Console[j].Text)
		}
		for j := range run.Tests {
			run.Tests[j].Name = mask(run.Tests[j].Name)
			run.Tests[j].Error = mask(run.Tests[j].Error)
		}
	}
	return s.log
}

// hasOutput reports whether a run has anything to show; a hook that ran
// silently (like the boilerplate's empty ones) stays out of the log.
func hasOutput(r models.ScriptRun) bool {
	return r.Error != "" || len(r.Console) > 0 || len(r.Tests) > 0
}

func (s *Session) context(run *models.ScriptRun) *Context {
	return &Context{env: s.env, vars: s.vars, run: run}
}
