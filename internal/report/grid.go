package report

import (
	"fmt"
	"io"
	"slices"
	"text/tabwriter"
	"time"

	"ai-eval/internal/eval"
)

type Axis struct {
	Target string
	Skills string
	Model  string
}

type Cell struct {
	Runs int           `json:"runs"`
	Pass int           `json:"pass"`
	Cost float64       `json:"cost"`
	P50  time.Duration `json:"p50_ns"`
}

type Grid struct {
	Rows    []string `json:"rows"`
	Columns []string `json:"columns"`
	Cells   [][]Cell `json:"cells"`
}

func BuildGrid(results []eval.Result, axes []Axis, minPass float64) Grid {
	var grid Grid
	for _, axis := range axes {
		if !slices.Contains(grid.Rows, axis.Skills) {
			grid.Rows = append(grid.Rows, axis.Skills)
		}
		if !slices.Contains(grid.Columns, axis.Model) {
			grid.Columns = append(grid.Columns, axis.Model)
		}
	}
	if i := slices.Index(grid.Rows, "none"); i > 0 {
		grid.Rows = slices.Insert(slices.Delete(grid.Rows, i, i+1), 0, "none")
	}
	grid.Cells = make([][]Cell, len(grid.Rows))
	for i := range grid.Cells {
		grid.Cells[i] = make([]Cell, len(grid.Columns))
	}
	return grid
}

func WriteGrid(w io.Writer, grid Grid) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "SKILLS \\ MODEL")
	for _, model := range grid.Columns {
		fmt.Fprintf(tw, "\t%s", model)
	}
	fmt.Fprintln(tw)
	for i, row := range grid.Rows {
		fmt.Fprint(tw, row)
		for j := range grid.Columns {
			fmt.Fprintf(tw, "\t%s", cellText(grid.Cells[i][j]))
		}
		fmt.Fprintln(tw)
	}
	tw.Flush()
}

func cellText(c Cell) string {
	return "-"
}
