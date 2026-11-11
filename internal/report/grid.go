package report

import (
	"fmt"
	"io"
	"slices"
	"strings"
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

type Pick struct {
	Skills string `json:"skills"`
	Model  string `json:"model"`
}

type Grid struct {
	Rows    []string `json:"rows"`
	Columns []string `json:"columns"`
	Cells   [][]Cell `json:"cells"`
	MinPass float64  `json:"min_pass,omitempty"`
	Pick    *Pick    `json:"pick,omitempty"`
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
	cellOf := map[string]*Cell{}
	for _, axis := range axes {
		row := slices.Index(grid.Rows, axis.Skills)
		column := slices.Index(grid.Columns, axis.Model)
		cellOf[axis.Target] = &grid.Cells[row][column]
	}
	walls := map[*Cell][]time.Duration{}
	for _, res := range results {
		cell, ok := cellOf[res.Target]
		if !ok {
			continue
		}
		cell.Runs++
		cell.Cost += res.Output.Trace.Cost
		if res.Err != "" {
			continue
		}
		if res.Pass {
			cell.Pass++
		}
		walls[cell] = append(walls[cell], res.Output.Trace.Wall)
	}
	for cell, w := range walls {
		slices.Sort(w)
		cell.P50 = percentile(w, 0.50)
	}
	grid.MinPass = minPass
	if minPass > 0 {
		grid.Pick = pickCell(grid, minPass)
	}
	return grid
}

func pickCell(grid Grid, minPass float64) *Pick {
	bestRow, bestColumn := -1, -1
	for i, row := range grid.Cells {
		for j, c := range row {
			if c.Runs == 0 || float64(c.Pass)/float64(c.Runs) < minPass {
				continue
			}
			if bestRow < 0 {
				bestRow, bestColumn = i, j
				continue
			}
			best := grid.Cells[bestRow][bestColumn]
			if c.Cost < best.Cost || (c.Cost == best.Cost && c.P50 < best.P50) {
				bestRow, bestColumn = i, j
			}
		}
	}
	if bestRow < 0 {
		return nil
	}
	return &Pick{Skills: grid.Rows[bestRow], Model: grid.Columns[bestColumn]}
}

func WriteGrid(w io.Writer, grid Grid) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "SKILLS \\ MODEL")
	for _, model := range grid.Columns {
		fmt.Fprintf(tw, "\t%s\t\t\t", model)
	}
	fmt.Fprintln(tw)
	for i, row := range grid.Rows {
		fmt.Fprint(tw, row)
		for j := range grid.Columns {
			parts := cellParts(grid.Cells[i][j])
			if grid.Pick != nil && grid.Pick.Skills == row && grid.Pick.Model == grid.Columns[j] {
				parts[0] = "* " + parts[0]
			}
			fmt.Fprintf(tw, "\t%s", strings.Join(parts, "\t"))
		}
		fmt.Fprintln(tw)
	}
	tw.Flush()
	percent := fmt.Sprintf("%.4g%%", grid.MinPass*100)
	if grid.Pick != nil {
		fmt.Fprintf(w, "* cheapest at ≥ %s pass: %s @ %s\n", percent, grid.Pick.Skills, grid.Pick.Model)
	} else if grid.MinPass > 0 {
		fmt.Fprintf(w, "no cell reached %s pass\n", percent)
	}
}

func cellParts(c Cell) []string {
	if c.Runs == 0 {
		return []string{"-", "", "", ""}
	}
	cost := "$0"
	if c.Cost >= 0.0001 {
		cost = fmt.Sprintf("$%.4f", c.Cost)
	} else if c.Cost > 0 {
		cost = "<$0.0001"
	}
	p50 := "-"
	if c.P50 > 0 {
		p50 = fmt.Sprintf("%.1fs", c.P50.Seconds())
	}
	return []string{fmt.Sprintf("%d/%d", c.Pass, c.Runs), fmt.Sprintf("%d%%", c.Pass*100/c.Runs), cost, p50}
}
