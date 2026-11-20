package mcp

import "testing"

func TestClassDefaults(t *testing.T) {
	p := NewPolicy(nil)
	want := map[Risk]bool{
		RiskRead: true, RiskOrganize: true,
		RiskDestructive: false, RiskExternal: false, RiskSensitive: false,
	}
	for risk, on := range want {
		if got := p.Allowed(ToolMeta{Name: "t", Risk: risk}); got != on {
			t.Errorf("%s: allowed=%v, want %v", risk, got, on)
		}
	}
}

func TestOverrideBeatsClassDefault(t *testing.T) {
	p := NewPolicy(map[string]bool{"send_request": true, "list_projects": false})
	if !p.Allowed(ToolMeta{Name: "send_request", Risk: RiskExternal}) {
		t.Error("an external tool switched on must be allowed")
	}
	if p.Allowed(ToolMeta{Name: "list_projects", Risk: RiskRead}) {
		t.Error("a read tool switched off must be refused")
	}
	if !p.Allowed(ToolMeta{Name: "list_scripts", Risk: RiskRead}) {
		t.Error("tools without an override follow their class")
	}
}

func TestSetReplacesOverrides(t *testing.T) {
	input := map[string]bool{"x": true}
	p := NewPolicy(input)
	input["x"] = false // the policy must hold its own copy
	if !p.Allowed(ToolMeta{Name: "x", Risk: RiskDestructive}) {
		t.Error("policy must not alias the caller's map")
	}
	p.Set(nil)
	if p.Allowed(ToolMeta{Name: "x", Risk: RiskDestructive}) {
		t.Error("Set(nil) must clear overrides")
	}
}
