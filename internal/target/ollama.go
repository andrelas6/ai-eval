package target

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ai-eval/internal/ollama"
	"ai-eval/internal/tool"
)

const maxTurns = 4

type Ollama struct {
	Client *ollama.Client
	Model  string
}

func (o Ollama) Name() string {
	return o.Model
}

func (o Ollama) Run(ctx context.Context, in Input) (Output, error) {
	var out Output
	if o.Model == "" {
		return out, errors.New("no model chosen")
	}

	tools := map[string]tool.Tool{}
	specs := make([]ollama.Tool, 0, len(in.Tools))
	for _, t := range in.Tools {
		spec := t.Spec()
		tools[spec.Name] = t
		specs = append(specs, toOllamaTool(spec))
	}

	var msgs []ollama.Message
	if in.System != "" {
		msgs = append(msgs, ollama.Message{Role: "system", Content: in.System})
	}
	msgs = append(msgs, ollama.Message{Role: "user", Content: in.Prompt})

	start := time.Now()
	for range maxTurns {
		turnStart := time.Now()
		resp, err := o.Client.Chat(ctx, ollama.ChatRequest{Model: o.Model, Messages: msgs, Tools: specs})
		turn := Turn{
			Duration:     time.Since(turnStart),
			EvalCount:    resp.EvalCount,
			EvalDuration: time.Duration(resp.EvalDuration),
		}
		if err != nil {
			out.Trace.Wall = time.Since(start)
			return out, err
		}
		msgs = append(msgs, resp.Message)

		if len(resp.Message.ToolCalls) == 0 {
			out.Answer = resp.Message.Content
			out.Trace.Turns = append(out.Trace.Turns, turn)
			out.Trace.Wall = time.Since(start)
			return out, nil
		}

		for _, tc := range resp.Message.ToolCalls {
			call := callTool(ctx, tools, tc.Function.Name, tc.Function.Arguments)
			turn.ToolCalls = append(turn.ToolCalls, call)
			content := call.Result
			if call.Err != "" {
				content = "error: " + call.Err
			}
			msgs = append(msgs, ollama.Message{Role: "tool", Content: content, ToolName: call.Name})
		}
		out.Trace.Turns = append(out.Trace.Turns, turn)
	}
	out.Trace.Wall = time.Since(start)
	return out, fmt.Errorf("no final answer after %d turns", maxTurns)
}

func callTool(ctx context.Context, tools map[string]tool.Tool, name string, args map[string]any) ToolCall {
	call := ToolCall{Name: name, Args: args}
	t, ok := tools[name]
	if !ok {
		call.Err = fmt.Sprintf("unknown tool %q", name)
		return call
	}
	result, err := t.Call(ctx, args)
	if err != nil {
		call.Err = err.Error()
		return call
	}
	call.Result = result
	return call
}

func toOllamaTool(s tool.Spec) ollama.Tool {
	return ollama.Tool{
		Type:     "function",
		Function: ollama.ToolFunction{Name: s.Name, Description: s.Description, Parameters: s.Parameters},
	}
}
