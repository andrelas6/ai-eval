package target

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-eval/internal/ollama"
	"ai-eval/internal/tool"
)

const sample = "../../testdata/sample"

const (
	answer      = `{"message":{"role":"assistant","content":"README.md and main.go"},"eval_count":20,"eval_duration":1000000000}`
	callListDir = `{"message":{"role":"assistant","tool_calls":[{"function":{"name":"list_dir","arguments":{"path":"."}}}]}}`
	callUnknown = `{"message":{"role":"assistant","tool_calls":[{"function":{"name":"rm_rf","arguments":{}}}]}}`
)

type fakeOllama struct {
	replies  []string
	requests []ollama.ChatRequest
	delay    time.Duration
}

func (f *fakeOllama) start(t *testing.T) *ollama.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollama.ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		f.requests = append(f.requests, req)
		time.Sleep(f.delay)
		i := min(len(f.requests), len(f.replies)) - 1
		w.Write([]byte(f.replies[i]))
	}))
	t.Cleanup(srv.Close)
	return &ollama.Client{Host: srv.URL, HTTP: srv.Client()}
}

func input() Input {
	return Input{
		System: "you can list files",
		Prompt: "list the files of this directory",
		Tools:  []tool.Tool{tool.ListDir{Root: sample}},
	}
}

// TestOllamaPlainAnswer checks the simplest run: the model answers right away with no tool call.
// It asserts the answer text comes back, the trace has exactly 1 turn, the wall and turn times
// are measured (the fake server waits 20ms, so both must be at least that), Ollama's token
// stats are copied into the turn, and the request sent the model name, system prompt and
// list_dir tool in Ollama's format.
func TestOllamaPlainAnswer(t *testing.T) {
	f := &fakeOllama{replies: []string{answer}, delay: 20 * time.Millisecond}
	o := Ollama{Client: f.start(t), Model: "m"}

	out, err := o.Run(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	if out.Answer != "README.md and main.go" {
		t.Errorf("answer = %q", out.Answer)
	}
	if len(out.Trace.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(out.Trace.Turns))
	}
	if out.Trace.Wall < 20*time.Millisecond || out.Trace.Turns[0].Duration < 20*time.Millisecond {
		t.Errorf("wall = %v, turn = %v, want >= 20ms", out.Trace.Wall, out.Trace.Turns[0].Duration)
	}
	if turn := out.Trace.Turns[0]; turn.EvalCount != 20 || turn.EvalDuration != time.Second {
		t.Errorf("turn = %+v", turn)
	}

	req := f.requests[0]
	if req.Model != "m" || req.Messages[0].Role != "system" || req.Messages[0].Content != "you can list files" {
		t.Errorf("request = %+v", req)
	}
	if len(req.Tools) != 1 || req.Tools[0].Type != "function" || req.Tools[0].Function.Name != "list_dir" {
		t.Errorf("tools sent = %+v", req.Tools)
	}
}

// TestOllamaToolCallThenAnswer checks the tool loop: the model first asks for list_dir, then answers.
// It asserts the trace has 2 turns, the first turn records the call (name, args and the real
// listing as result), and the second request sends that result back as a "tool" message.
func TestOllamaToolCallThenAnswer(t *testing.T) {
	f := &fakeOllama{replies: []string{callListDir, answer}}
	o := Ollama{Client: f.start(t), Model: "m"}

	out, err := o.Run(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Trace.Turns) != 2 {
		t.Fatalf("turns = %d, want 2", len(out.Trace.Turns))
	}
	calls := out.Trace.Turns[0].ToolCalls
	if len(calls) != 1 || calls[0].Name != "list_dir" || calls[0].Args["path"] != "." {
		t.Fatalf("tool calls = %+v", calls)
	}
	if !strings.Contains(calls[0].Result, "README.md") || calls[0].Err != "" {
		t.Errorf("tool result = %+v", calls[0])
	}

	last := f.requests[1].Messages[len(f.requests[1].Messages)-1]
	if last.Role != "tool" || last.ToolName != "list_dir" || !strings.Contains(last.Content, "README.md") {
		t.Errorf("tool message sent back = %+v", last)
	}
}

// TestOllamaUnknownToolKeepsGoing checks a model calling a tool we never offered.
// It asserts the run doesn't stop: the error is recorded in the trace and sent back to the
// model as "error: ...", so it gets a chance to recover and still answer.
func TestOllamaUnknownToolKeepsGoing(t *testing.T) {
	f := &fakeOllama{replies: []string{callUnknown, answer}}
	o := Ollama{Client: f.start(t), Model: "m"}

	out, err := o.Run(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	call := out.Trace.Turns[0].ToolCalls[0]
	if call.Err != `unknown tool "rm_rf"` {
		t.Errorf("err = %q", call.Err)
	}
	if last := f.requests[1].Messages[len(f.requests[1].Messages)-1]; last.Content != `error: unknown tool "rm_rf"` {
		t.Errorf("tool message sent back = %q", last.Content)
	}
}

// TestOllamaToolErrorIsSentBack checks a real tool failing, here list_dir asked to leave the fixture.
// It asserts the run continues, the tool's error is recorded in the trace, and the same error
// is sent back to the model as a "tool" message.
func TestOllamaToolErrorIsSentBack(t *testing.T) {
	bad := `{"message":{"role":"assistant","tool_calls":[{"function":{"name":"list_dir","arguments":{"path":"../.."}}}]}}`
	f := &fakeOllama{replies: []string{bad, answer}}
	o := Ollama{Client: f.start(t), Model: "m"}

	out, err := o.Run(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	if call := out.Trace.Turns[0].ToolCalls[0]; !strings.Contains(call.Err, "escapes") {
		t.Errorf("err = %q", call.Err)
	}
	want := "error: path escapes the current directory: ../.."
	if last := f.requests[1].Messages[len(f.requests[1].Messages)-1]; last.Role != "tool" || last.Content != want {
		t.Errorf("tool message sent back = %+v, want content %q", last, want)
	}
}

// TestOllamaGivesUpAfterMaxTurns checks a model that keeps calling tools and never answers.
// It asserts the run stops after 4 turns with a clear error, and the trace still holds all
// 4 turns and the time spent, so the failed run can be inspected.
func TestOllamaGivesUpAfterMaxTurns(t *testing.T) {
	f := &fakeOllama{replies: []string{callListDir}}
	o := Ollama{Client: f.start(t), Model: "m"}

	out, err := o.Run(context.Background(), input())
	if err == nil || err.Error() != "no final answer after 4 turns" {
		t.Fatalf("err = %v", err)
	}
	if len(out.Trace.Turns) != 4 || out.Trace.Wall == 0 {
		t.Errorf("trace = %+v", out.Trace)
	}
}

// TestOllamaHTTPError checks Ollama returning an error status.
// It asserts the error from the server (e.g. "does not support tools") reaches the caller.
func TestOllamaHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model does not support tools", http.StatusInternalServerError)
	}))
	defer srv.Close()
	o := Ollama{Client: &ollama.Client{Host: srv.URL, HTTP: srv.Client()}, Model: "m"}

	_, err := o.Run(context.Background(), input())
	if err == nil || !strings.Contains(err.Error(), "does not support tools") {
		t.Fatalf("err = %v", err)
	}
}

// TestOllamaNeedsAModel checks the model has to be chosen explicitly.
// It asserts a target with no model fails with "no model chosen" instead of picking a default.
func TestOllamaNeedsAModel(t *testing.T) {
	o := Ollama{Client: &ollama.Client{}}
	if _, err := o.Run(context.Background(), input()); err == nil || err.Error() != "no model chosen" {
		t.Fatalf("err = %v", err)
	}
}

// TestOllamaName checks the target is named after its model, which is how it shows up in reports.
func TestOllamaName(t *testing.T) {
	if got := (Ollama{Model: "qwen3.5:9b"}).Name(); got != "qwen3.5:9b" {
		t.Errorf("name = %q", got)
	}
}
