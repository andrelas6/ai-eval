package grader

import (
	"fmt"
	"strings"

	"ai-eval/internal/target"
)

type ToolCalled struct {
	Tool string
}

func (ToolCalled) Name() string {
	return "tool_called"
}

func (g ToolCalled) Grade(out target.Output) Score {
	s := Score{Grader: g.Name()}
	count := 0
	var others []string
	for _, c := range out.Trace.ToolCalls() {
		if c.Name == g.Tool {
			count++
		} else {
			others = append(others, c.Name)
		}
	}

	if count == 0 {
		called := "nothing"
		if len(others) > 0 {
			called = strings.Join(others, ", ")
		}
		s.Note = fmt.Sprintf("%s never called (called: %s)", g.Tool, called)
		return s
	}
	s.Value = 1
	s.Pass = true
	s.Note = fmt.Sprintf("called %s %dx", g.Tool, count)
	return s
}
