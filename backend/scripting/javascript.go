package scripting

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// javascript.go — the JavaScript engine (goja, pure Go). A fresh runtime per
// script per send: it starts with plain JavaScript only, and gets console,
// req/res and ctx from here. goja has no modules, so a script defines its
// hooks as plain functions named beforeRequest and afterResponse.

type javaScript struct {
	limit time.Duration
	done  <-chan struct{}
}

type jsProgram struct {
	vm     *goja.Runtime
	limit  time.Duration
	done   <-chan struct{}
	ctx    *Context // the call in progress; console writes here
	before goja.Callable
	after  goja.Callable
	expect goja.Value
}

// prelude is defined in every runtime before the script: ctx.expect, a small
// assertion helper (Chai would be far bigger than scripts need).
const prelude = `
function __snaprqExpect(actual) {
  const show = (v) => { try { return JSON.stringify(v); } catch (e) { return String(v); } };
  const fail = (what) => { throw new Error('expected ' + show(actual) + ' ' + what); };
  return {
    toBe: (expected) => { if (actual !== expected) fail('to be ' + show(expected)); },
    toEqual: (expected) => { if (show(actual) !== show(expected)) fail('to equal ' + show(expected)); },
    toBeTruthy: () => { if (!actual) fail('to be truthy'); },
    toBeFalsy: () => { if (actual) fail('to be falsy'); },
    toContain: (item) => { if (actual == null || !actual.includes(item)) fail('to contain ' + show(item)); },
    toBeGreaterThan: (n) => { if (!(actual > n)) fail('to be greater than ' + show(n)); },
    toBeLessThan: (n) => { if (!(actual < n)) fail('to be less than ' + show(n)); },
  };
}
`

func (e javaScript) Load(script Script, ctx *Context) (Program, error) {
	p := &jsProgram{vm: goja.New(), limit: e.limit, done: e.done, ctx: ctx}
	p.installConsole()
	if _, err := p.vm.RunString(prelude); err != nil {
		return nil, fmt.Errorf("preparing the script runtime: %w", err)
	}
	p.expect = p.vm.Get("__snaprqExpect")

	if err := p.guard(func() error {
		_, err := p.vm.RunScript(script.Name, script.Source)
		return err
	}); err != nil {
		return nil, err
	}
	p.before, _ = goja.AssertFunction(p.vm.Get(PhaseBefore))
	p.after, _ = goja.AssertFunction(p.vm.Get(PhaseAfter))
	return p, nil
}

func (p *jsProgram) Has(phase string) bool {
	switch phase {
	case PhaseBefore:
		return p.before != nil
	case PhaseAfter:
		return p.after != nil
	}
	return false
}

func (p *jsProgram) BeforeRequest(req *Request, ctx *Context) error {
	p.ctx = ctx
	vm := p.vm
	reqObj := vm.NewObject()
	_ = reqObj.Set("method", req.Method)
	_ = reqObj.Set("url", req.URL)
	_ = reqObj.Set("body", req.Body)
	_ = reqObj.Set("headers", p.headersObject(req.Headers, true))

	if err := p.guard(func() error {
		_, err := p.before(goja.Undefined(), reqObj, p.contextObject(ctx))
		return err
	}); err != nil {
		return err
	}
	req.Method = stringProp(reqObj, "method", req.Method)
	req.URL = stringProp(reqObj, "url", req.URL)
	req.Body = stringProp(reqObj, "body", req.Body)
	return nil
}

func (p *jsProgram) AfterResponse(res *Response, ctx *Context) error {
	p.ctx = ctx
	vm := p.vm
	resObj := vm.NewObject()
	_ = resObj.Set("status", res.Status)
	_ = resObj.Set("body", res.Body)
	_ = resObj.Set("durationMs", res.DurationMs)
	_ = resObj.Set("headers", p.headersObject(res.Headers, false))
	_ = resObj.Set("json", func() goja.Value {
		var parsed any
		if err := json.Unmarshal([]byte(res.Body), &parsed); err != nil {
			panic(vm.NewTypeError("the response body is not JSON: %v", err))
		}
		return vm.ToValue(parsed)
	})

	return p.guard(func() error {
		_, err := p.after(goja.Undefined(), resObj, p.contextObject(ctx))
		return err
	})
}

// headersObject exposes headers as get/all, plus set/remove when writable.
func (p *jsProgram) headersObject(h *Headers, writable bool) *goja.Object {
	obj := p.vm.NewObject()
	_ = obj.Set("get", func(name string) goja.Value {
		if v, ok := h.Get(name); ok {
			return p.vm.ToValue(v)
		}
		return goja.Undefined()
	})
	_ = obj.Set("all", func() map[string]string { return h.All() })
	if writable {
		_ = obj.Set("set", func(name, value string) { h.Set(name, value) })
		_ = obj.Set("remove", func(name string) { h.Remove(name) })
	}
	return obj
}

// contextObject is ctx as scripts see it.
func (p *jsProgram) contextObject(ctx *Context) *goja.Object {
	vm := p.vm
	obj := vm.NewObject()

	env := vm.NewObject()
	_ = env.Set("get", func(key string) goja.Value {
		if v, ok := ctx.EnvGet(key); ok {
			return vm.ToValue(v)
		}
		return goja.Undefined()
	})
	_ = obj.Set("env", env)

	vars := vm.NewObject()
	_ = vars.Set("get", func(key string) goja.Value {
		if v, ok := ctx.Var(key); ok {
			return vm.ToValue(v)
		}
		return goja.Undefined()
	})
	_ = vars.Set("set", func(key string, value goja.Value) { ctx.SetVar(key, value.Export()) })
	_ = obj.Set("vars", vars)

	_ = obj.Set("test", func(name string, value goja.Value) {
		fn, ok := goja.AssertFunction(value)
		if !ok {
			ctx.Test(name, false, "ctx.test needs a function")
			return
		}
		result, err := fn(goja.Undefined())
		var interrupted *goja.InterruptedError
		switch {
		case errors.As(err, &interrupted):
			ctx.Test(name, false, "timed out")
			vm.Interrupt(interrupted.Value()) // keep stopping the script
		case err != nil:
			ctx.Test(name, false, testFailure(err))
		case result != nil && result.ExportType() != nil && result.Export() == false:
			ctx.Test(name, false, "returned false")
		default:
			ctx.Test(name, true, "")
		}
	})
	_ = obj.Set("expect", p.expect)
	_ = obj.Set("skip", func(reason string) { ctx.Skip(reason) })
	return obj
}

func (p *jsProgram) installConsole() {
	console := p.vm.NewObject()
	for _, level := range []string{"log", "info", "warn", "error"} {
		_ = console.Set(level, func(call goja.FunctionCall) goja.Value {
			parts := make([]string, len(call.Arguments))
			for i, arg := range call.Arguments {
				parts[i] = p.show(arg)
			}
			p.ctx.Log(level, strings.Join(parts, " "))
			return goja.Undefined()
		})
	}
	_ = p.vm.Set("console", console)
}

// show formats a console argument: strings as they are, objects as JSON.
func (p *jsProgram) show(v goja.Value) string {
	if v == nil || goja.IsUndefined(v) {
		return "undefined"
	}
	if goja.IsNull(v) {
		return "null"
	}
	if _, isObject := v.(*goja.Object); isObject {
		if _, isFunction := goja.AssertFunction(v); !isFunction {
			if raw, err := json.Marshal(v.Export()); err == nil {
				return string(raw)
			}
		}
	}
	return v.String()
}

// guard runs fn under the time limit and turns goja's errors into messages
// naming the script and line.
func (p *jsProgram) guard(fn func() error) error {
	timer := time.AfterFunc(p.limit, func() {
		p.vm.Interrupt(fmt.Sprintf("timed out after %s", p.limit))
	})
	finished := make(chan struct{})
	go func() {
		select {
		case <-p.done:
			p.vm.Interrupt(ErrCancelled)
		case <-finished:
		}
	}()
	err := fn()
	close(finished)
	timer.Stop()
	p.vm.ClearInterrupt()
	if err == nil {
		return nil
	}
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		if interrupted.Value() == ErrCancelled {
			return ErrCancelled
		}
		return fmt.Errorf("timed out after %s", p.limit)
	}
	return errors.New(errorMessage(err))
}

// errorMessage is a JavaScript error's message with where it happened.
func errorMessage(err error) string {
	var exception *goja.Exception
	if errors.As(err, &exception) {
		return strings.TrimSpace(exception.Error())
	}
	return err.Error()
}

// testFailure is why a test failed: the thrown error's message alone (an
// expect failure's location would point into the prelude, not the script).
func testFailure(err error) string {
	var exception *goja.Exception
	if errors.As(err, &exception) {
		if obj, ok := exception.Value().(*goja.Object); ok {
			if message := obj.Get("message"); message != nil && !goja.IsUndefined(message) {
				return message.String()
			}
		}
		return exception.Value().String()
	}
	return err.Error()
}

// stringProp reads a string property a script may have changed; one it
// deleted or set to null keeps the old value.
func stringProp(obj *goja.Object, name, fallback string) string {
	v := obj.Get(name)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return fallback
	}
	return v.String()
}
