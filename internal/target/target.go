package target

import (
	"context"
	"time"

	"ai-eval/internal/tool"
)

type Target interface {
	Name() string
	Run(ctx context.Context, in Input) (Output, error)
}

type Input struct {
	System string
	Prompt string
	Tools  []tool.Tool
}

type Output struct {
	Answer string `json:"answer"`
	Trace  Trace  `json:"trace"`
}

type Trace struct {
	Turns []Turn        `json:"turns"`
	Wall  time.Duration `json:"wall_ns"`
}

type Turn struct {
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	Duration     time.Duration `json:"duration_ns"`
	EvalCount    int           `json:"eval_count"`
	EvalDuration time.Duration `json:"eval_duration_ns"`
}

type ToolCall struct {
	Name   string         `json:"name"`
	Args   map[string]any `json:"args"`
	Result string         `json:"result"`
	Err    string         `json:"error,omitempty"`
}

func (t Trace) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, turn := range t.Turns {
		calls = append(calls, turn.ToolCalls...)
	}
	return calls
}
