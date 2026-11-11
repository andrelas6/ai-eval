package report

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ai-eval/internal/eval"
	"ai-eval/internal/grader"
	"ai-eval/internal/target"
)

func result(tgt, cs string, wall time.Duration, pass bool, scores ...grader.Score) eval.Result {
	turn := target.Turn{EvalCount: 50, EvalDuration: time.Second}
	return eval.Result{
		Target: tgt,
		Case:   cs,
		Pass:   pass,
		Tries:  1,
		Scores: scores,
		Output: target.Output{Trace: target.Trace{Wall: wall, Turns: []target.Turn{turn}}},
	}
}

func failed(tgt, cs string) eval.Result {
	return eval.Result{Target: tgt, Case: cs, Err: "HTTP 500", Tries: 1}
}

func score(name string, value float64, pass bool) grader.Score {
	return grader.Score{Grader: name, Value: value, Pass: pass}
}

// TestSummarizeGroupsRunsByTargetAndCase checks the numbers behind each table row.
// It asserts runs are grouped per target and case in the order first seen, and each row
// counts passes and errors, per-grader passes and average values, nearest-rank p50 and p95
// over the runs that didn't error, and the average tokens per second.
func TestSummarizeGroupsRunsByTargetAndCase(t *testing.T) {
	results := []eval.Result{
		result("m1", "c1", 1*time.Second, true, score("files", 1, true)),
		result("m1", "c1", 3*time.Second, false, score("files", 0.5, false)),
		failed("m1", "c1"),
		result("m1", "c1", 2*time.Second, true, score("files", 1, true)),
		result("m2", "c1", 5*time.Second, true, score("files", 1, true)),
	}

	rows := Summarize(results)

	if len(rows) != 2 || rows[0].Target != "m1" || rows[1].Target != "m2" {
		t.Fatalf("rows = %+v", rows)
	}
	r := rows[0]
	if r.Case != "c1" || r.Runs != 4 || r.Pass != 2 || r.Errors != 1 {
		t.Errorf("counts = %+v", r)
	}
	if g := r.Graders["files"]; g.Runs != 3 || g.Pass != 2 || g.AvgValue != 2.5/3 || g.Partial != true {
		t.Errorf("files = %+v", g)
	}
	if r.P50 != 2*time.Second || r.P95 != 3*time.Second {
		t.Errorf("p50 = %v, p95 = %v", r.P50, r.P95)
	}
	if r.TokPerSec != 50 {
		t.Errorf("tok/s = %v", r.TokPerSec)
	}
}

// TestSummarizeAllRunsErrored checks a target that never answered.
// It asserts the row still shows up with zero passes and zero times instead of crashing.
func TestSummarizeAllRunsErrored(t *testing.T) {
	rows := Summarize([]eval.Result{failed("m1", "c1"), failed("m1", "c1")})
	if len(rows) != 1 || rows[0].Errors != 2 || rows[0].P50 != 0 || rows[0].TokPerSec != 0 {
		t.Errorf("rows = %+v", rows)
	}
}

// TestWriteTable checks the printed table.
// It asserts grader columns come from the graders that were used, a grader cell shows
// passes plus the average when scores are partial, "-" marks a grader a case doesn't use,
// and rows are sorted by case, then most passes, then fastest p50.
func TestWriteTable(t *testing.T) {
	results := []eval.Result{
		result("slow", "c1", 4*time.Second, true, score("files", 1, true), score("latency", 1, true)),
		result("fast", "c1", 1*time.Second, true, score("files", 1, true), score("latency", 1, true)),
		result("bad", "c1", 1*time.Second, false, score("files", 0.5, false), score("latency", 1, true)),
		result("fast", "c2", 1*time.Second, true, score("tool_called", 1, true)),
	}

	var buf bytes.Buffer
	WriteTable(&buf, Summarize(results))

	var lines [][]string
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		lines = append(lines, strings.Fields(l))
	}
	want := [][]string{
		{"TARGET", "CASE", "PASS", "ERR", "files", "latency", "tool_called", "p50(s)", "p95(s)", "tok/s", "RETRIES", "COST", "SKILLS"},
		{"fast", "c1", "1/1", "0", "1/1", "1/1", "-", "1.0", "1.0", "50", "-", "-", "-"},
		{"slow", "c1", "1/1", "0", "1/1", "1/1", "-", "4.0", "4.0", "50", "-", "-", "-"},
		{"bad", "c1", "0/1", "0", "0/1", "(0.50)", "1/1", "-", "1.0", "1.0", "50", "-", "-", "-"},
		{"fast", "c2", "1/1", "0", "-", "-", "1/1", "1.0", "1.0", "50", "-", "-", "-"},
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("table:\n%s", buf.String())
	}
}

// TestWriteJSON checks the full results and the grid are saved for reading failures later.
// It asserts the file lands in the given folder (created if missing) named after the time,
// and reading it back gives an object whose results match the input, trace included, and whose grid matches too.
func TestWriteJSON(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "results")
	results := []eval.Result{result("m1", "c1", time.Second, true, score("files", 1, true))}
	results[0].Output.Answer = "README.md"
	results[0].Output.Trace.Turns[0].ToolCalls = []target.ToolCall{{Name: "list_dir", Args: map[string]any{"path": "."}, Result: "README.md"}}

	grid := Grid{Rows: []string{"none"}, Columns: []string{"m1"}, Cells: [][]Cell{{{}}}}

	path, err := WriteJSON(dir, results, grid, time.Date(2026, 10, 10, 9, 30, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "20261010-093005.json") {
		t.Errorf("path = %q", path)
	}

	b, _ := os.ReadFile(path)
	var back struct {
		Results []eval.Result `json:"results"`
		Grid    Grid          `json:"grid"`
	}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Results, results) {
		t.Errorf("round trip:\n got  %+v\n want %+v", back.Results, results)
	}
	if !reflect.DeepEqual(back.Grid, grid) {
		t.Errorf("grid:\n got  %+v\n want %+v", back.Grid, grid)
	}
}

func piResult(tries int, cost float64, offered, loaded []string, errText string) eval.Result {
	return eval.Result{
		Target: "pi+fin@m",
		Case:   "c1",
		Tries:  tries,
		Err:    errText,
		Pass:   errText == "",
		Output: target.Output{Trace: target.Trace{
			Wall:          time.Second,
			Cost:          cost,
			SkillsOffered: offered,
			SkillsLoaded:  loaded,
			Turns:         []target.Turn{{}},
		}},
	}
}

// TestSummarizeCountsSkillsTriesAndCost checks the Pi columns behind a row.
// It asserts retries (tries beyond the first) add up across runs, cost is summed, and each offered skill is counted
// per run with how many runs loaded it, with runs that errored still counted.
func TestSummarizeCountsSkillsTriesAndCost(t *testing.T) {
	fin := []string{"fin"}
	rows := Summarize([]eval.Result{
		piResult(1, 0.0005, fin, fin, ""),
		piResult(2, 0.0007, fin, fin, ""),
		piResult(3, 0.0003, fin, nil, "skill not loaded: fin"),
	})

	r := rows[0]
	if r.Retries != 3 || math.Abs(r.Cost-0.0015) > 1e-12 {
		t.Errorf("retries = %d, cost = %v", r.Retries, r.Cost)
	}
	if len(r.Skills) != 1 || r.Skills[0] != (SkillStats{Name: "fin", Offered: 3, Loaded: 2}) {
		t.Errorf("skills = %+v", r.Skills)
	}
}

// TestWriteTableFillsThePiColumns checks the new cells for a Pi row.
// It asserts SKILLS shows each skill with loaded/offered runs, RETRIES the retries, COST
// the sum with four decimals, and tok/s is "-" because Pi has no eval duration.
func TestWriteTableFillsThePiColumns(t *testing.T) {
	two := []string{"fin", "tax"}
	rows := Summarize([]eval.Result{
		piResult(1, 0.0005, two, two, ""),
		piResult(2, 0.0007, two, []string{"fin"}, ""),
		piResult(1, 0.0000, two, two, ""),
	})

	var buf bytes.Buffer
	WriteTable(&buf, rows)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	cells := strings.Fields(lines[1])
	if !strings.HasSuffix(strings.Join(cells, " "), "- 1 $0.0012 fin 3/3, tax 2/3") || cells[6] != "-" {
		t.Errorf("table:\n%s", buf.String())
	}
}

// TestWriteTableShowsDashesForOllamaRows checks rows with no skills or cost.
// It asserts an Ollama-style row shows "-" for RETRIES, COST and SKILLS.
func TestWriteTableShowsDashesForOllamaRows(t *testing.T) {
	var buf bytes.Buffer
	WriteTable(&buf, Summarize([]eval.Result{result("m1", "c1", time.Second, true, score("files", 1, true))}))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if !strings.HasSuffix(strings.Join(strings.Fields(lines[1]), " "), "50 - - -") {
		t.Errorf("table:\n%s", buf.String())
	}
}

type retryThenPass struct {
	calls int
}

func (r *retryThenPass) Name() string { return "pi+fin@m" }

func (r *retryThenPass) Run(ctx context.Context, in target.Input) (target.Output, error) {
	r.calls++
	trace := target.Trace{Cost: 0.001, Wall: time.Second}
	if r.calls <= 2 {
		return target.Output{Trace: trace}, target.RetryError{Reason: "again"}
	}
	return target.Output{Answer: "ok", Trace: trace}, nil
}

// TestSummarizeCountsTheCostOfRetriedTries checks the runner and report agree on cost.
// It asserts a run that needed two retries, each try costing 0.001, shows cost 0.003 and 2 retries.
func TestSummarizeCountsTheCostOfRetriedTries(t *testing.T) {
	c := eval.Case{ID: "c1", Prompt: "q", Runs: 1}
	results := eval.Runner{Timeout: time.Second, Retries: 2}.Run(context.Background(), []target.Target{&retryThenPass{}}, []eval.Case{c})

	row := Summarize(results)[0]

	if math.Abs(row.Cost-0.003) > 1e-12 || row.Retries != 2 {
		t.Errorf("cost = %v, retries = %d", row.Cost, row.Retries)
	}
}

// TestWriteTableShowsTinyCosts checks a cost under a hundredth of a cent.
// It asserts a cost above 0 but under $0.0001 shows as "<$0.0001", not "$0.0000".
func TestWriteTableShowsTinyCosts(t *testing.T) {
	var buf bytes.Buffer
	WriteTable(&buf, []Row{{Target: "t", Case: "c", Runs: 1, Cost: 0.00004}})

	if !strings.Contains(buf.String(), "<$0.0001") {
		t.Errorf("table:\n%s", buf.String())
	}
}
