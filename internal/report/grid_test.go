package report

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
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
