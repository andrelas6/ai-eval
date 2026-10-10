package eval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"ai-eval/internal/grader"
	"ai-eval/internal/target"
)

type fakeTarget struct {
	name   string
	answer string
	err    error
	block  bool
	inputs []target.Input
}

func (f *fakeTarget) Name() string { return f.name }

func (f *fakeTarget) Run(ctx context.Context, in target.Input) (target.Output, error) {
	f.inputs = append(f.inputs, in)
	if f.block {
		<-ctx.Done()
		return target.Output{}, ctx.Err()
	}
	return target.Output{Answer: f.answer, Trace: target.Trace{Wall: time.Millisecond}}, f.err
}

type answerIs string

func (answerIs) Name() string { return "answer_is" }

func (a answerIs) Grade(out target.Output) grader.Score {
	pass := out.Answer == string(a)
	return grader.Score{Grader: "answer_is", Pass: pass}
}

func testCase(id string, runs int, graders ...grader.Grader) Case {
	return Case{ID: id, System: "sys", Prompt: "do it", Runs: runs, Graders: graders}
}

// TestRunnerRunsEveryTargetOnEveryCase checks the run matrix.
// It asserts 2 targets x 1 case x 3 runs gives 6 results, grouped by target in order,
// numbered 1 to 3, each carrying the case id, the output and the grader scores.
func TestRunnerRunsEveryTargetOnEveryCase(t *testing.T) {
	a := &fakeTarget{name: "a", answer: "ok"}
	b := &fakeTarget{name: "b", answer: "ok"}
	r := Runner{Timeout: time.Second}

	results := r.Run(context.Background(), []target.Target{a, b}, []Case{testCase("c1", 3, answerIs("ok"))})

	var got []string
	for _, res := range results {
		got = append(got, fmt.Sprintf("%s/%s/%d", res.Target, res.Case, res.N))
		if res.Output.Answer != "ok" || len(res.Scores) != 1 || !res.Pass {
			t.Errorf("result = %+v", res)
		}
	}
	if want := "a/c1/1 a/c1/2 a/c1/3 b/c1/1 b/c1/2 b/c1/3"; strings.Join(got, " ") != want {
		t.Errorf("order = %v, want %s", got, want)
	}
}

// TestRunnerSendsTheCaseToTheTarget checks what each run asks the target to do.
// It asserts the case's system prompt, prompt and tools reach the target unchanged.
func TestRunnerSendsTheCaseToTheTarget(t *testing.T) {
	a := &fakeTarget{name: "a"}
	c := testCase("c1", 1, answerIs(""))

	Runner{Timeout: time.Second}.Run(context.Background(), []target.Target{a}, []Case{c})

	in := a.inputs[len(a.inputs)-1]
	if in.System != "sys" || in.Prompt != "do it" {
		t.Errorf("input = %+v", in)
	}
}

// TestRunnerWarmsUpEachTargetOnce checks a model is loaded before its scored runs.
// It asserts each target gets exactly one extra "hi" call before anything else, across all
// its cases, and that call does not show up in the results.
func TestRunnerWarmsUpEachTargetOnce(t *testing.T) {
	a := &fakeTarget{name: "a"}
	cases := []Case{testCase("c1", 2, answerIs("")), testCase("c2", 1, answerIs(""))}

	results := Runner{Timeout: time.Second}.Run(context.Background(), []target.Target{a}, cases)

	if len(a.inputs) != 4 || a.inputs[0].Prompt != "hi" || len(results) != 3 {
		t.Fatalf("calls = %d (first %q), results = %d", len(a.inputs), a.inputs[0].Prompt, len(results))
	}
	for _, in := range a.inputs[1:] {
		if in.Prompt == "hi" {
			t.Error("warm-up should only happen once")
		}
	}
}

// TestRunnerLogsAFailedWarmUpAndKeepsGoing checks a warm-up error doesn't stop the eval.
// It asserts the failure is logged with the target's name and the scored runs still happen.
func TestRunnerLogsAFailedWarmUpAndKeepsGoing(t *testing.T) {
	a := &fakeTarget{name: "a", err: errors.New("model not found")}
	var logs []string
	r := Runner{Timeout: time.Second, Logf: func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}}

	results := r.Run(context.Background(), []target.Target{a}, []Case{testCase("c1", 2, answerIs(""))})

	if len(logs) != 1 || logs[0] != "a: warm-up failed: model not found" {
		t.Errorf("logs = %q", logs)
	}
	if len(results) != 2 {
		t.Errorf("results = %d, want 2", len(results))
	}
}

// TestRunnerRecordsTargetErrors checks a run where the target fails.
// It asserts the error is kept on the result, the run is not graded and fails, and the
// next runs still happen.
func TestRunnerRecordsTargetErrors(t *testing.T) {
	a := &fakeTarget{name: "a", err: errors.New("HTTP 500")}

	results := Runner{Timeout: time.Second}.Run(context.Background(), []target.Target{a}, []Case{testCase("c1", 2, answerIs(""))})

	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	for _, res := range results {
		if res.Err != "HTTP 500" || res.Pass || len(res.Scores) != 0 {
			t.Errorf("result = %+v", res)
		}
	}
}

// TestRunnerPassNeedsEveryGrader checks one failing grader fails the run.
// It asserts all scores are kept and pass is false when any grader fails.
func TestRunnerPassNeedsEveryGrader(t *testing.T) {
	a := &fakeTarget{name: "a", answer: "ok"}
	c := testCase("c1", 1, answerIs("ok"), answerIs("something else"))

	res := Runner{Timeout: time.Second}.Run(context.Background(), []target.Target{a}, []Case{c})[0]

	if len(res.Scores) != 2 || res.Pass {
		t.Errorf("result = %+v", res)
	}
}

// TestRunnerTimesOutASlowRun checks the per-run time limit.
// It asserts a target that never answers is stopped with a deadline error, and every run
// gets its own fresh limit instead of sharing one.
func TestRunnerTimesOutASlowRun(t *testing.T) {
	a := &fakeTarget{name: "a", block: true}

	results := Runner{Timeout: 20 * time.Millisecond}.Run(context.Background(), []target.Target{a}, []Case{testCase("c1", 2, answerIs(""))})

	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	for _, res := range results {
		if res.Err != context.DeadlineExceeded.Error() {
			t.Errorf("err = %q", res.Err)
		}
	}
}

// TestRunnerStopsWhenCancelled checks Ctrl-C stops the eval instead of running everything.
// It asserts that cancelling after the first result returns just that result, and
// OnResult was called for it.
func TestRunnerStopsWhenCancelled(t *testing.T) {
	a := &fakeTarget{name: "a"}
	b := &fakeTarget{name: "b"}
	ctx, cancel := context.WithCancel(context.Background())
	var seen []Result
	r := Runner{Timeout: time.Second, OnResult: func(res Result) {
		seen = append(seen, res)
		cancel()
	}}

	results := r.Run(ctx, []target.Target{a, b}, []Case{testCase("c1", 3, answerIs(""))})

	if len(results) != 1 || len(seen) != 1 || len(b.inputs) != 0 {
		t.Errorf("results = %d, seen = %d, b calls = %d", len(results), len(seen), len(b.inputs))
	}
}
