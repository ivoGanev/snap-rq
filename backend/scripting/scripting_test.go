package scripting

import (
	"context"
	"strings"
	"testing"
	"time"
)

type mapEnv map[string]string

func (m mapEnv) Get(key string) (string, bool) { v, ok := m[key]; return v, ok }

func session(limit time.Duration, sources ...string) *Session {
	scripts := make([]Script, len(sources))
	for i, source := range sources {
		scripts[i] = Script{ID: int64(i + 1), Name: "s" + string(rune('1'+i)), Source: source}
	}
	s := NewSession(context.Background(), scripts, mapEnv{"HOST": "example.com"})
	s.engine = javaScript{limit: limit}
	return s
}

func TestAStuckScriptIsStoppedAtTheTimeLimit(t *testing.T) {
	for _, source := range []string{
		`while (true) {}`,
		`function beforeRequest(req, ctx) { while (true) {} }`,
	} {
		s := session(50*time.Millisecond, source)
		start := time.Now()
		_, err := s.BeforeRequest(&Request{Headers: ParseHeaders("")})
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Errorf("%q: error = %v, want a timeout", source, err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("%q: stopped after %s", source, elapsed)
		}
	}
}

func TestTheSessionKeepsGoingAfterAScriptTimesOut(t *testing.T) {
	s := session(50*time.Millisecond, `function afterResponse(res, ctx) { while (true) {} }`,
		`function afterResponse(res, ctx) { console.log("next"); }`)
	if _, err := s.BeforeRequest(&Request{Headers: ParseHeaders("")}); err != nil {
		t.Fatal(err)
	}
	s.AfterResponse(&Response{Status: 200, Headers: ParseHeaders("")})
	log := s.Log(func(text string) string { return text })
	if len(log) != 2 || !strings.Contains(log[0].Error, "timed out") || log[1].Console[0].Text != "next" {
		t.Errorf("log = %+v", log)
	}
}

func TestErrorsNameTheLine(t *testing.T) {
	s := session(time.Second, "function beforeRequest(req, ctx) {\n  const x = 1;\n  x.nope.deeper;\n}")
	_, err := s.BeforeRequest(&Request{Headers: ParseHeaders("")})
	if err == nil || !strings.Contains(err.Error(), ":3:") {
		t.Errorf("error = %v, want it to point at line 3", err)
	}
}

func TestTheSandboxHasNoWayOut(t *testing.T) {
	s := session(time.Second, `function beforeRequest(req, ctx) {
  ctx.test("no require", () => ctx.expect(typeof require).toBe("undefined"));
  ctx.test("no fetch", () => ctx.expect(typeof fetch).toBe("undefined"));
  ctx.test("no process", () => ctx.expect(typeof process).toBe("undefined"));
  ctx.test("env reads", () => ctx.expect(ctx.env.get("HOST")).toBe("example.com"));
  ctx.test("unknown env", () => ctx.expect(ctx.env.get("NOPE")).toBe(undefined));
}`)
	if _, err := s.BeforeRequest(&Request{Headers: ParseHeaders("")}); err != nil {
		t.Fatal(err)
	}
	for _, test := range s.Log(func(text string) string { return text })[0].Tests {
		if !test.Passed {
			t.Errorf("%s: %s", test.Name, test.Error)
		}
	}
}

func TestEachHookGetsAFreshLogButOneSandbox(t *testing.T) {
	s := session(time.Second, `let calls = 0;
function beforeRequest(req, ctx) { calls++; }
function afterResponse(res, ctx) { calls++; console.log("calls", calls); }`)
	if _, err := s.BeforeRequest(&Request{Headers: ParseHeaders("")}); err != nil {
		t.Fatal(err)
	}
	s.AfterResponse(&Response{Status: 200, Headers: ParseHeaders("")})
	log := s.Log(func(text string) string { return text })
	if len(log) != 1 || log[0].Console[0].Text != "calls 2" {
		t.Errorf("log = %+v, want afterResponse to see beforeRequest's state", log)
	}
}

func TestCheckFindsTheFirstSyntaxError(t *testing.T) {
	if problems := Check(Boilerplate); len(problems) != 0 {
		t.Errorf("the boilerplate has problems: %+v", problems)
	}
	problems := Check("function beforeRequest(req, ctx) {\n  req.url = ;\n}\nlet = = 2;")
	if len(problems) != 1 || problems[0].Line != 2 {
		t.Errorf("problems = %+v, want one on line 2", problems)
	}
	if problems := Check("const a = 1;\nconst a = 2;"); len(problems) != 1 || problems[0].Line != 2 {
		t.Errorf("duplicate declaration: %+v", problems)
	}
}
