package report

import (
	"bytes"
	"encoding/json"
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
		Scores: scores,
		Output: target.Output{Trace: target.Trace{Wall: wall, Turns: []target.Turn{turn}}},
	}
}

func failed(tgt, cs string) eval.Result {
	return eval.Result{Target: tgt, Case: cs, Err: "HTTP 500"}
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
		{"TARGET", "CASE", "PASS", "ERR", "files", "latency", "tool_called", "p50(s)", "p95(s)", "tok/s"},
		{"fast", "c1", "1/1", "0", "1/1", "1/1", "-", "1.0", "1.0", "50"},
		{"slow", "c1", "1/1", "0", "1/1", "1/1", "-", "4.0", "4.0", "50"},
		{"bad", "c1", "0/1", "0", "0/1", "(0.50)", "1/1", "-", "1.0", "1.0", "50"},
		{"fast", "c2", "1/1", "0", "-", "-", "1/1", "1.0", "1.0", "50"},
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("table:\n%s", buf.String())
	}
}

// TestWriteJSON checks the full results are saved for reading failures later.
// It asserts the file lands in the given folder (created if missing) named after the time,
// and reading it back gives the same results, trace included.
func TestWriteJSON(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "results")
	results := []eval.Result{result("m1", "c1", time.Second, true, score("files", 1, true))}
	results[0].Output.Answer = "README.md"
	results[0].Output.Trace.Turns[0].ToolCalls = []target.ToolCall{{Name: "list_dir", Args: map[string]any{"path": "."}, Result: "README.md"}}

	path, err := WriteJSON(dir, results, time.Date(2026, 10, 10, 9, 30, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "20261010-093005.json") {
		t.Errorf("path = %q", path)
	}

	b, _ := os.ReadFile(path)
	var back []eval.Result
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, results) {
		t.Errorf("round trip:\n got  %+v\n want %+v", back, results)
	}
}
