package mcp

import "sync"

// classDefaults is whether a risk class is available when the user has not
// overridden a tool. Anything not listed is off.
var classDefaults = map[Risk]bool{
	RiskRead:     true,
	RiskOrganize: true,
}

// ClassDefault reports whether a risk class is on by default.
func ClassDefault(r Risk) bool { return classDefaults[r] }

// Policy decides which tools are available: a per-tool override wins, then
// the class default.
type Policy struct {
	mu        sync.RWMutex
	overrides map[string]bool
}

// NewPolicy returns a policy with the given per-tool overrides.
func NewPolicy(overrides map[string]bool) *Policy {
	p := &Policy{}
	p.Set(overrides)
	return p
}

// Set replaces all per-tool overrides.
func (p *Policy) Set(overrides map[string]bool) {
	copied := make(map[string]bool, len(overrides))
	for name, allowed := range overrides {
		copied[name] = allowed
	}
	p.mu.Lock()
	p.overrides = copied
	p.mu.Unlock()
}

// Allowed reports whether the tool may be listed and called right now.
func (p *Policy) Allowed(t ToolMeta) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if allowed, ok := p.overrides[t.Name]; ok {
		return allowed
	}
	return classDefaults[t.Risk]
}
