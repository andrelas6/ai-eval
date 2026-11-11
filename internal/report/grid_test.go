package report

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
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

func gridLines(t *testing.T, results []eval.Result, minPass float64) []string {
	t.Helper()
	var out bytes.Buffer
	WriteGrid(&out, BuildGrid(results, gridAxes(), minPass))
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
}

// TestPickIsEmptyWithoutMinPass checks a run without -min-pass.
// It asserts the grid has no pick and no min_pass in the JSON, and the printout has no star and no pick line.
func TestPickIsEmptyWithoutMinPass(t *testing.T) {
	results := []eval.Result{result("pi@p/a", "c", time.Second, true)}
	grid := BuildGrid(results, gridAxes(), 0)
	b, err := json.Marshal(grid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "pick") || strings.Contains(string(b), "min_pass") {
		t.Errorf("json = %s", b)
	}
	lines := gridLines(t, results, 0)
	if len(lines) != 3 || strings.Contains(strings.Join(lines, "\n"), "*") {
		t.Errorf("lines = %q", lines)
	}
}

// TestPickTakesCheapestQualifyingCell checks the pick among cells above the threshold.
// It asserts the cheapest passing cell is picked, a cheaper cell below the threshold is skipped,
// the star is on the x / p/a cell and not on x / p/b, and the line under the grid names the skills and the model.
func TestPickTakesCheapestQualifyingCell(t *testing.T) {
	results := []eval.Result{
		costed("pi@p/a", "c", time.Second, true, 0.05),
		costed("pi+x@p/a", "c", time.Second, true, 0.02),
		costed("pi@p/b", "c", time.Second, false, 0.001),
		costed("pi+x@p/b", "c", time.Second, true, 0.03),
	}
	lines := gridLines(t, results, 0.9)
	if len(lines) != 4 {
		t.Fatalf("lines = %q", lines)
	}
	wantRow := []string{"x", "*", "1/1", "100%", "$0.0200", "1.0s", "1/1", "100%", "$0.0300", "1.0s"}
	if !slices.Equal(strings.Fields(lines[2]), wantRow) || strings.Contains(lines[1], "*") {
		t.Errorf("rows = %q", lines[1:3])
	}
	if lines[3] != "* cheapest at ≥ 90% pass: x @ p/a" {
		t.Errorf("line = %q", lines[3])
	}
}

// TestPickPrefersFreeCell checks a cell with no cost against a paid cell.
// It asserts the $0 cell wins.
func TestPickPrefersFreeCell(t *testing.T) {
	results := []eval.Result{
		costed("pi@p/a", "c", time.Second, true, 0.01),
		costed("pi+x@p/b", "c", time.Second, true, 0),
	}
	lines := gridLines(t, results, 0.5)
	if lines[len(lines)-1] != "* cheapest at ≥ 50% pass: x @ p/b" {
		t.Errorf("lines = %q", lines)
	}
}

// TestPickBreaksCostTieByP50 checks two cells with the same cost.
// It asserts the cell with the lower median time wins, whichever comes first.
func TestPickBreaksCostTieByP50(t *testing.T) {
	results := []eval.Result{
		costed("pi@p/a", "c", 5*time.Second, true, 0.01),
		costed("pi+x@p/b", "c", 2*time.Second, true, 0.01),
	}
	lines := gridLines(t, results, 0.9)
	if lines[len(lines)-1] != "* cheapest at ≥ 90% pass: x @ p/b" {
		t.Errorf("lines = %q", lines)
	}
}

// TestPickQualifiesExactlyAtThreshold checks the real pass fraction is used.
// It asserts 9 of 10 qualifies at 90%, and 2 of 3 qualifies at 66.5% although it prints 66%, but not at 66.7%.
func TestPickQualifiesExactlyAtThreshold(t *testing.T) {
	var results []eval.Result
	for i := 0; i < 10; i++ {
		results = append(results, costed("pi@p/a", "c", time.Second, i != 0, 0.01))
	}
	lines := gridLines(t, results, 0.9)
	if lines[len(lines)-1] != "* cheapest at ≥ 90% pass: none @ p/a" {
		t.Errorf("lines = %q", lines)
	}
	two := []eval.Result{
		costed("pi@p/a", "c", time.Second, true, 0.01),
		costed("pi@p/a", "c", time.Second, true, 0.01),
		costed("pi@p/a", "c", time.Second, false, 0.01),
	}
	if grid := BuildGrid(two, gridAxes(), 0.665); grid.Pick == nil {
		t.Errorf("2 of 3 should qualify at 66.5%%")
	}
	if grid := BuildGrid(two, gridAxes(), 0.667); grid.Pick != nil {
		t.Errorf("pick = %+v, want none at 66.7%%", grid.Pick)
	}
}

// TestPickSaysWhenNoCellQualifies checks a threshold nobody reaches.
// It asserts no star, a line saying no cell reached 90%, and no pick in the JSON.
func TestPickSaysWhenNoCellQualifies(t *testing.T) {
	results := []eval.Result{
		result("pi@p/a", "c", time.Second, false),
		failed("pi+x@p/a", "c"),
	}
	lines := gridLines(t, results, 0.9)
	if strings.Contains(strings.Join(lines[:3], "\n"), "*") || lines[len(lines)-1] != "no cell reached 90% pass" {
		t.Errorf("lines = %q", lines)
	}
	grid := BuildGrid(results, gridAxes(), 0.9)
	b, err := json.Marshal(grid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "pick") || !strings.Contains(string(b), `"min_pass":0.9`) {
		t.Errorf("json = %s", b)
	}
}
