package scripting

import (
	"errors"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"
)

// Problem is a mistake in a script's code, at a 1-based line and column.
type Problem struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// Check compiles a script's code the way the engine will, without running
// anything, and returns its first syntax error (none when it compiles). Only
// the first: after one, goja's parser loses its place and reports follow-on
// errors that aren't real. Using the engine's own parser means it also flags
// syntax goja doesn't support.
func Check(source string) []Problem {
	program, err := parser.ParseFile(nil, "script", source, 0)
	if err != nil {
		var list parser.ErrorList
		if errors.As(err, &list) && len(list) > 0 {
			first := list[0]
			return []Problem{{Line: first.Position.Line, Column: first.Position.Column, Message: first.Message}}
		}
		return []Problem{{Line: 1, Column: 1, Message: err.Error()}}
	}

	// Parsed: compiling catches the rest (e.g. assigning to a constant
	// expression, or a duplicate let).
	if _, err := goja.CompileAST(program, false); err != nil {
		return []Problem{compileProblem(err)}
	}
	return nil
}

func compileProblem(err error) Problem {
	var syntax *goja.CompilerSyntaxError
	var reference *goja.CompilerReferenceError
	var ce *goja.CompilerError
	switch {
	case errors.As(err, &syntax):
		ce = &syntax.CompilerError
	case errors.As(err, &reference):
		ce = &reference.CompilerError
	}
	if ce == nil || ce.File == nil {
		return Problem{Line: 1, Column: 1, Message: err.Error()}
	}
	position := ce.File.Position(ce.Offset)
	return Problem{Line: position.Line, Column: position.Column, Message: ce.Message}
}
