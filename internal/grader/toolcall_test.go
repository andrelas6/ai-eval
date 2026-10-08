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

// TestToolCalledPassesWhenTheToolWasUsed checks the trace is searched for the tool.
// It asserts a run that called list_dir (even among other calls) passes with value 1,
// and the note says how many times it was called.
func TestToolCalledPassesWhenTheToolWasUsed(t *testing.T) {
	s := ToolCalled{Tool: "list_dir"}.Grade(withCalls("other", "list_dir", "list_dir"))
	if s.Grader != "tool_called" || !s.Pass || s.Value != 1 || s.Note != "called list_dir 2x" {
		t.Errorf("score = %+v", s)
	}
}

// TestToolCalledFailsWhenTheToolWasNotUsed checks a model that answered from memory or
// called something else.
// It asserts the run fails with value 0, and the note says what was called instead.
func TestToolCalledFailsWhenTheToolWasNotUsed(t *testing.T) {
	s := ToolCalled{Tool: "list_dir"}.Grade(withCalls("rm_rf"))
	if s.Pass || s.Value != 0 || s.Note != "list_dir never called (called: rm_rf)" {
		t.Errorf("score = %+v", s)
	}

	s = ToolCalled{Tool: "list_dir"}.Grade(target.Output{})
	if s.Pass || s.Note != "list_dir never called (called: nothing)" {
		t.Errorf("no calls: score = %+v", s)
	}
}
