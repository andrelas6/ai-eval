package grader

import (
	"testing"

	"ai-eval/internal/target"
)

func withCalls(names ...string) target.Output {
	var turn target.Turn
	for _, n := range names {
		turn.ToolCalls = append(turn.ToolCalls, target.ToolCall{Name: n})
	}
	return target.Output{Trace: target.Trace{Turns: []target.Turn{turn}}}
}

// TestToolCalledSearchesTheTrace checks the trace is searched for the tool.
// Each row asserts pass, value and note: a run that called list_dir (even among other calls)
// passes with value 1 and says how often; a run that called something else or nothing fails
// with value 0 and says what was called instead.
func TestToolCalledSearchesTheTrace(t *testing.T) {
	g := ToolCalled{Tool: "list_dir"}

	tests := []struct {
		name  string
		out   target.Output
		pass  bool
		value float64
		note  string
	}{
		{"called among others", withCalls("other", "list_dir", "list_dir"), true, 1, "called list_dir 2x"},
		{"called something else", withCalls("rm_rf"), false, 0, "list_dir never called (called: rm_rf)"},
		{"called nothing", target.Output{}, false, 0, "list_dir never called (called: nothing)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := g.Grade(tc.out)
			if s.Grader != "tool_called" || s.Pass != tc.pass || s.Value != tc.value || s.Note != tc.note {
				t.Fatalf("got %+v, want pass=%v value=%v note=%q", s, tc.pass, tc.value, tc.note)
			}
		})
	}
}
