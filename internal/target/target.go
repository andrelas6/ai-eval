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
	Dir    string
}

type RetryError struct {
	Reason string
}

func (e RetryError) Error() string { return e.Reason }

type Output struct {
	Answer string            `json:"answer"`
	Trace  Trace             `json:"trace"`
	Files  map[string][]byte `json:"-"`
}

type Trace struct {
	Turns         []Turn        `json:"turns"`
	Wall          time.Duration `json:"wall_ns"`
	Agent         string        `json:"agent,omitempty"`
	SkillsOffered []string      `json:"skills_offered,omitempty"`
	SkillsLoaded  []string      `json:"skills_loaded,omitempty"`
	Tokens        int           `json:"tokens"`
	Cost          float64       `json:"cost"`
	Sandbox       string        `json:"sandbox,omitempty"`
	FilesErr      string        `json:"files_error,omitempty"`
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
