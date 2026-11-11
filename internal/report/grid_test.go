package report

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"ai-eval/internal/eval"
)

func gridAxes() []Axis {
	return []Axis{
		{Target: "pi@p/a", Skills: "none", Model: "p/a"},
		{Target: "pi+x@p/a", Skills: "x", Model: "p/a"},
		{Target: "pi@p/b", Skills: "none", Model: "p/b"},
		{Target: "pi+x@p/b", Skills: "x", Model: "p/b"},
	}
}

// TestBuildGridFillsRowsAndColumns checks the grid frame.
// It asserts rows are the skill sets with none first, columns are the models in axis order,
// and there is one empty cell for each row and column, even with no results.
func TestBuildGridFillsRowsAndColumns(t *testing.T) {
	axes := []Axis{
		{Target: "pi+x@p/a", Skills: "x", Model: "p/a"},
		{Target: "pi@p/a", Skills: "none", Model: "p/a"},
		{Target: "pi+x@p/b", Skills: "x", Model: "p/b"},
		{Target: "pi@p/b", Skills: "none", Model: "p/b"},
	}
	grid := BuildGrid(nil, axes, 0)
	want := Grid{
		Rows:    []string{"none", "x"},
		Columns: []string{"p/a", "p/b"},
		Cells:   [][]Cell{{{}, {}}, {{}, {}}},
	}
	if !reflect.DeepEqual(grid, want) {
		t.Errorf("grid = %+v, want %+v", grid, want)
	}
}

// TestBuildGridKeepsFlagOrderAfterNone checks the order of several skill sets.
// It asserts rows follow the order of the axes, with none moved to the front.
func TestBuildGridKeepsFlagOrderAfterNone(t *testing.T) {
	axes := []Axis{
		{Target: "t1", Skills: "b", Model: "m"},
		{Target: "t2", Skills: "a,c", Model: "m"},
		{Target: "t3", Skills: "none", Model: "m"},
	}
	grid := BuildGrid(nil, axes, 0)
	if !reflect.DeepEqual(grid.Rows, []string{"none", "b", "a,c"}) {
		t.Errorf("rows = %v", grid.Rows)
	}
}

// TestWriteGridPrintsFrame checks the printed grid.
// It asserts one header line with the corner and models, one line per skill set,
// and a dash in every cell.
func TestWriteGridPrintsFrame(t *testing.T) {
	var out bytes.Buffer
	WriteGrid(&out, BuildGrid(nil, gridAxes(), 0))
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	wantFields := [][]string{
		{"SKILLS", `\`, "MODEL", "p/a", "p/b"},
		{"none", "-", "-"},
		{"x", "-", "-"},
	}
	for i, line := range lines {
		if !reflect.DeepEqual(strings.Fields(line), wantFields[i]) {
			t.Errorf("line %d = %q", i, line)
		}
	}
}

func costed(tgt, cs string, wall time.Duration, pass bool, cost float64) eval.Result {
	res := result(tgt, cs, wall, pass)
	res.Output.Trace.Cost = cost
	return res
}

// TestBuildGridCountsRunsAndPasses checks the counts of one pair over several cases and runs.
// It asserts Runs covers every run, an errored run adds to Runs but not Pass,
// results of other pairs do not leak into the cell, and a result with an unknown target is ignored.
func TestBuildGridCountsRunsAndPasses(t *testing.T) {
	results := []eval.Result{
		result("pi@p/a", "c1", time.Second, true),
		result("pi@p/a", "c1", time.Second, false),
		result("pi@p/a", "c2", time.Second, true),
		failed("pi@p/a", "c2"),
		result("pi+x@p/a", "c1", time.Second, true),
		result("pi@other", "c1", time.Second, true),
	}
	grid := BuildGrid(results, gridAxes(), 0)
	if c := grid.Cells[0][1]; c != (Cell{}) {
		t.Errorf("none/p/b = %+v, want empty", c)
	}
	if c := grid.Cells[0][0]; c.Runs != 4 || c.Pass != 2 {
		t.Errorf("none/p/a = %+v, want 2 of 4", c)
	}
	if c := grid.Cells[1][0]; c.Runs != 1 || c.Pass != 1 {
		t.Errorf("x/p/a = %+v, want 1 of 1", c)
	}
}

// TestBuildGridSumsCost checks the cost of a cell.
// It asserts the cost adds every run, errored runs included, and stays 0 when none is reported.
func TestBuildGridSumsCost(t *testing.T) {
	errored := failed("pi@p/a", "c1")
	errored.Output.Trace.Cost = 0.5
	results := []eval.Result{
		costed("pi@p/a", "c1", time.Second, true, 0.25),
		costed("pi@p/a", "c1", time.Second, true, 0.25),
		errored,
		result("pi@p/b", "c1", time.Second, true),
	}
	grid := BuildGrid(results, gridAxes(), 0)
	if got := grid.Cells[0][0].Cost; got != 1.0 {
		t.Errorf("cost = %v, want 1", got)
	}
	if got := grid.Cells[0][1].Cost; got != 0 {
		t.Errorf("cost = %v, want 0", got)
	}
}

// TestBuildGridP50MatchesTable checks the median time of a cell.
// It asserts the cell p50 equals the table p50 for the same runs and ignores errored runs.
func TestBuildGridP50MatchesTable(t *testing.T) {
	results := []eval.Result{
		result("pi@p/a", "c1", 1*time.Second, true),
		result("pi@p/a", "c1", 3*time.Second, true),
		failed("pi@p/a", "c1"),
		result("pi@p/a", "c1", 2*time.Second, true),
	}
	grid := BuildGrid(results, gridAxes(), 0)
	want := Summarize(results)[0].P50
	if got := grid.Cells[0][0].P50; got != want || got == 0 {
		t.Errorf("p50 = %v, want %v", got, want)
	}
}

// TestWriteGridPrintsCellNumbers checks the printed cells.
// It asserts a filled cell reads x/N, percent, cost and p50, a cell without cost reads $0,
// and a pair with no results stays a dash while other cells have numbers.
func TestWriteGridPrintsCellNumbers(t *testing.T) {
	results := []eval.Result{
		costed("pi@p/a", "c1", 40100*time.Millisecond, true, 0.005),
		costed("pi@p/a", "c1", 40100*time.Millisecond, true, 0.007),
		costed("pi@p/a", "c1", 40100*time.Millisecond, true, 0),
		costed("pi@p/a", "c1", 40100*time.Millisecond, false, 0),
		result("pi@p/b", "c1", 2*time.Second, true),
	}
	var out bytes.Buffer
	WriteGrid(&out, BuildGrid(results, gridAxes(), 0))
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	want := [][]string{
		{"none", "3/4", "75%", "$0.0120", "40.1s", "1/1", "100%", "$0", "2.0s"},
		{"x", "-", "-"},
	}
	for i, line := range lines[1:] {
		if !reflect.DeepEqual(strings.Fields(line), want[i]) {
			t.Errorf("line %d = %q", i+1, line)
		}
	}
}

func printedGrid(results []eval.Result) []string {
	var out bytes.Buffer
	WriteGrid(&out, BuildGrid(results, gridAxes(), 0))
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
}

// TestWriteGridRoundsPercentDown checks the percent of a partly passing cell.
// It asserts 2 of 3 prints 66% and 199 of 200 prints 99%, so 100% means every run passed.
func TestWriteGridRoundsPercentDown(t *testing.T) {
	results := []eval.Result{
		result("pi@p/a", "c1", time.Second, true),
		result("pi@p/a", "c1", time.Second, true),
		result("pi@p/a", "c1", time.Second, false),
	}
	for i := 0; i < 200; i++ {
		results = append(results, result("pi@p/b", "c1", time.Second, i != 0))
	}
	fields := strings.Fields(printedGrid(results)[1])
	if fields[2] != "66%" || fields[6] != "99%" {
		t.Errorf("fields = %q", fields)
	}
}

// TestWriteGridPrintsTinyCost checks a cost below one hundredth of a cent.
// It asserts the cell reads <$0.0001 like the table.
func TestWriteGridPrintsTinyCost(t *testing.T) {
	results := []eval.Result{costed("pi@p/a", "c1", time.Second, true, 0.00001)}
	fields := strings.Fields(printedGrid(results)[1])
	if fields[3] != "<$0.0001" {
		t.Errorf("fields = %q", fields)
	}
}

// TestWriteGridPrintsDashWhenEveryRunErrored checks a cell with runs but no finished run.
// It asserts the counts show and the p50 reads a dash.
func TestWriteGridPrintsDashWhenEveryRunErrored(t *testing.T) {
	results := []eval.Result{failed("pi@p/a", "c1"), failed("pi@p/a", "c1")}
	fields := strings.Fields(printedGrid(results)[1])
	if fields[1] != "0/2" || fields[4] != "-" {
		t.Errorf("fields = %q", fields)
	}
}

// TestWriteGridAlignsCellParts checks the columns inside cells.
// It asserts the percent of a short count and a long count start at the same column.
func TestWriteGridAlignsCellParts(t *testing.T) {
	results := []eval.Result{result("pi+x@p/a", "c1", time.Second, true)}
	for i := 0; i < 12; i++ {
		results = append(results, result("pi@p/a", "c1", time.Second, true))
	}
	lines := printedGrid(results)
	a := strings.Index(lines[1], "100%")
	b := strings.Index(lines[2], "100%")
	if a < 0 || a != b {
		t.Errorf("percent columns %d and %d in %q", a, b, lines)
	}
}
