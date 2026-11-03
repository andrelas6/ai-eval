package eval

import (
	"context"
	"errors"
	"fmt"
	"math"
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

type warmTarget struct {
	*fakeTarget
}

func (w warmTarget) WarmUp(ctx context.Context) error {
	_, err := w.fakeTarget.Run(ctx, target.Input{Prompt: "hi"})
	return err
}

type flakyTarget struct {
	name     string
	failures int
	reason   error
	calls    int
}

func (f *flakyTarget) Name() string { return f.name }

func (f *flakyTarget) Run(ctx context.Context, in target.Input) (target.Output, error) {
	f.calls++
	cost := target.Trace{Cost: 0.001, Tokens: 10}
	if f.calls <= f.failures {
		return target.Output{Trace: cost}, f.reason
	}
	return target.Output{Answer: "ok", Trace: cost}, nil
}

type slowThenFineTarget struct {
	calls int
}

func (s *slowThenFineTarget) Name() string { return "slow" }

func (s *slowThenFineTarget) Run(ctx context.Context, in target.Input) (target.Output, error) {
	s.calls++
	if s.calls == 1 {
		<-ctx.Done()
		return target.Output{}, target.RetryError{Reason: "too slow"}
	}
	if err := ctx.Err(); err != nil {
		return target.Output{}, err
	}
	return target.Output{Answer: "ok"}, nil
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
// It asserts a target with a WarmUp method gets exactly one extra "hi" call before anything else, across all
// its cases, and that call does not show up in the results.
func TestRunnerWarmsUpEachTargetOnce(t *testing.T) {
	a := &fakeTarget{name: "a"}
	cases := []Case{testCase("c1", 2, answerIs("")), testCase("c2", 1, answerIs(""))}

	results := Runner{Timeout: time.Second}.Run(context.Background(), []target.Target{warmTarget{a}}, cases)

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

	results := r.Run(context.Background(), []target.Target{warmTarget{a}}, []Case{testCase("c1", 2, answerIs(""))})

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

// TestRunnerSkipsWarmUpWithoutAWarmUpMethod checks warm-up is opt-in.
// It asserts a target without WarmUp only sees the scored runs.
func TestRunnerSkipsWarmUpWithoutAWarmUpMethod(t *testing.T) {
	a := &fakeTarget{name: "a"}

	Runner{Timeout: time.Second}.Run(context.Background(), []target.Target{a}, []Case{testCase("c1", 2, answerIs(""))})

	if len(a.inputs) != 2 {
		t.Fatalf("calls = %d, want 2", len(a.inputs))
	}
	for _, in := range a.inputs {
		if in.Prompt == "hi" {
			t.Error("unexpected warm-up call")
		}
	}
}

// TestRunnerPassesTheFixtureAsDir checks the target is told where to work.
// It asserts Input.Dir is the case's fixture folder.
func TestRunnerPassesTheFixtureAsDir(t *testing.T) {
	a := &fakeTarget{name: "a"}
	c := testCase("c1", 1, answerIs(""))
	c.Fixture = "some/fixture"

	Runner{Timeout: time.Second}.Run(context.Background(), []target.Target{a}, []Case{c})

	if a.inputs[0].Dir != "some/fixture" {
		t.Errorf("dir = %q", a.inputs[0].Dir)
	}
}

// TestRunnerRetriesARetryError checks retryable failures are tried again.
// It asserts two RetryErrors then a good answer passes on the third try with Tries = 3.
func TestRunnerRetriesARetryError(t *testing.T) {
	f := &flakyTarget{name: "f", failures: 2, reason: target.RetryError{Reason: "skill not loaded: x"}}

	res := Runner{Timeout: time.Second, Retries: 2}.Run(context.Background(), []target.Target{f}, []Case{testCase("c1", 1, answerIs("ok"))})[0]

	if res.Err != "" || !res.Pass || res.Tries != 3 || f.calls != 3 {
		t.Fatalf("result = %+v, calls = %d", res, f.calls)
	}
}

// TestRunnerGivesEachTryAFreshTimeout checks the time limit is per try.
// It asserts a first try that waits out its Timeout and fails with a RetryError doesn't
// stop the second try from running with a live context and passing.
func TestRunnerGivesEachTryAFreshTimeout(t *testing.T) {
	s := &slowThenFineTarget{}

	res := Runner{Timeout: 20 * time.Millisecond, Retries: 1}.Run(context.Background(), []target.Target{s}, []Case{testCase("c1", 1, answerIs("ok"))})[0]

	if res.Err != "" || res.Tries != 2 {
		t.Errorf("result = %+v", res)
	}
}

// TestRunnerAlwaysTriesOnce checks a negative Retries can't skip the run.
// It asserts the target is called once and the result still names the target and case.
func TestRunnerAlwaysTriesOnce(t *testing.T) {
	a := &fakeTarget{name: "a", answer: "ok"}

	res := Runner{Timeout: time.Second, Retries: -1}.Run(context.Background(), []target.Target{a}, []Case{testCase("c1", 1, answerIs("ok"))})[0]

	if len(a.inputs) != 1 || res.Target != "a" || res.Case != "c1" || res.Tries != 1 {
		t.Errorf("result = %+v, calls = %d", res, len(a.inputs))
	}
}

// TestRunnerLogsEachRetry checks retries are visible.
// It asserts one log line per failed try that will be retried, naming target, case, try and reason.
func TestRunnerLogsEachRetry(t *testing.T) {
	f := &flakyTarget{name: "f", failures: 2, reason: target.RetryError{Reason: "skill not loaded: x"}}
	var logs []string
	r := Runner{Timeout: time.Second, Retries: 2, Logf: func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}}

	r.Run(context.Background(), []target.Target{f}, []Case{testCase("c1", 1, answerIs("ok"))})

	want := []string{
		"f c1: try 1 failed, retrying: skill not loaded: x",
		"f c1: try 2 failed, retrying: skill not loaded: x",
	}
	if strings.Join(logs, "|") != strings.Join(want, "|") {
		t.Errorf("logs = %q", logs)
	}
}

// TestRunnerGivesUpAfterTheRetries checks the retry limit.
// It asserts 1 + Retries tries are made, then the run is an error that carries the reason.
func TestRunnerGivesUpAfterTheRetries(t *testing.T) {
	f := &flakyTarget{name: "f", failures: 99, reason: target.RetryError{Reason: "skill not loaded: x"}}

	res := Runner{Timeout: time.Second, Retries: 2}.Run(context.Background(), []target.Target{f}, []Case{testCase("c1", 1, answerIs("ok"))})[0]

	if f.calls != 3 || res.Tries != 3 || res.Pass || res.Err != "skill not loaded: x" {
		t.Errorf("result = %+v, calls = %d", res, f.calls)
	}
}

// TestRunnerDoesNotRetryAPlainError checks only RetryError is retried.
// It asserts a plain error is tried once, even with Retries set.
func TestRunnerDoesNotRetryAPlainError(t *testing.T) {
	f := &flakyTarget{name: "f", failures: 99, reason: errors.New("HTTP 500")}

	res := Runner{Timeout: time.Second, Retries: 2}.Run(context.Background(), []target.Target{f}, []Case{testCase("c1", 1, answerIs("ok"))})[0]

	if f.calls != 1 || res.Tries != 1 || res.Err != "HTTP 500" {
		t.Errorf("result = %+v, calls = %d", res, f.calls)
	}
}

// TestRunnerCountsOneTryWhenItWorks checks Tries on a first-time pass.
// It asserts Tries is 1.
func TestRunnerCountsOneTryWhenItWorks(t *testing.T) {
	a := &fakeTarget{name: "a", answer: "ok"}

	res := Runner{Timeout: time.Second, Retries: 2}.Run(context.Background(), []target.Target{a}, []Case{testCase("c1", 1, answerIs("ok"))})[0]

	if res.Tries != 1 {
		t.Errorf("tries = %d", res.Tries)
	}
}

func piProcess(streams ...string) (func(ctx context.Context, bin string, args []string, dir string) ([]byte, []byte, error), *int) {
	calls := 0
	return func(ctx context.Context, bin string, args []string, dir string) ([]byte, []byte, error) {
		i := min(calls, len(streams)-1)
		calls++
		return []byte(streams[i]), nil, nil
	}, &calls
}

const (
	piWithoutSkill = `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"stopReason":"stop"}}
{"type":"agent_end","messages":[]}
`
	piWithSkill = `{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","id":"c1","name":"read","arguments":{"path":"/s/skills/fin/SKILL.md"}}],"stopReason":"toolUse"}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"stopReason":"stop"}}
{"type":"agent_end","messages":[]}
`
)

// TestRunnerRetriesPiUntilTheSkillIsLoaded checks the skill check works through the runner.
// It asserts a Pi run that skips the skill on try 1 and reads it on try 2 passes with Tries 2.
func TestRunnerRetriesPiUntilTheSkillIsLoaded(t *testing.T) {
	process, calls := piProcess(piWithoutSkill, piWithSkill)
	p := target.Pi{Model: "x/m", Skills: []string{"/s/skills/fin"}, Process: process}

	res := Runner{Timeout: time.Second, Retries: 2}.Run(context.Background(), []target.Target{p}, []Case{testCase("c1", 1, answerIs("ok"))})[0]

	if res.Err != "" || !res.Pass || res.Tries != 2 || *calls != 2 {
		t.Errorf("result = %+v, calls = %d", res, *calls)
	}
}

// TestRunnerFailsPiWhenTheSkillIsNeverLoaded checks the retry limit for skills.
// It asserts a skill that is never read ends as "skill not loaded: fin" after 3 tries.
func TestRunnerFailsPiWhenTheSkillIsNeverLoaded(t *testing.T) {
	process, calls := piProcess(piWithoutSkill)
	p := target.Pi{Model: "x/m", Skills: []string{"/s/skills/fin"}, Process: process}

	res := Runner{Timeout: time.Second, Retries: 2}.Run(context.Background(), []target.Target{p}, []Case{testCase("c1", 1, answerIs("ok"))})[0]

	if res.Err != "skill not loaded: fin" || res.Pass || res.Tries != 3 || *calls != 3 {
		t.Errorf("result = %+v, calls = %d", res, *calls)
	}
}

// TestRunnerAddsUpCostAndTokensOverAllTries checks retries are paid for in the result.
// It asserts two RetryError tries then a pass, each costing 0.001 and 10 tokens, give a
// result with cost 0.003 and 30 tokens.
func TestRunnerAddsUpCostAndTokensOverAllTries(t *testing.T) {
	f := &flakyTarget{name: "f", failures: 2, reason: target.RetryError{Reason: "again"}}

	res := Runner{Timeout: time.Second, Retries: 2}.Run(context.Background(), []target.Target{f}, []Case{testCase("c1", 1, answerIs("ok"))})[0]

	if math.Abs(res.Output.Trace.Cost-0.003) > 1e-12 || res.Output.Trace.Tokens != 30 || res.Tries != 3 {
		t.Errorf("cost = %v, tokens = %d, tries = %d", res.Output.Trace.Cost, res.Output.Trace.Tokens, res.Tries)
	}
}
