package report

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"ai-eval/internal/eval"
)

type GraderStats struct {
	Runs     int
	Pass     int
	AvgValue float64
	Partial  bool
}

type Row struct {
	Target    string
	Case      string
	Runs      int
	Pass      int
	Errors    int
	Graders   map[string]GraderStats
	P50       time.Duration
	P95       time.Duration
	TokPerSec float64
}

func Summarize(results []eval.Result) []Row {
	var rows []Row
	index := map[[2]string]int{}
	walls := map[int][]time.Duration{}
	speeds := map[int][]float64{}

	for _, res := range results {
		key := [2]string{res.Target, res.Case}
		i, ok := index[key]
		if !ok {
			i = len(rows)
			index[key] = i
			rows = append(rows, Row{Target: res.Target, Case: res.Case, Graders: map[string]GraderStats{}})
		}
		row := &rows[i]
		row.Runs++
		if res.Err != "" {
			row.Errors++
			continue
		}
		if res.Pass {
			row.Pass++
		}
		for _, s := range res.Scores {
			g := row.Graders[s.Grader]
			g.Runs++
			if s.Pass {
				g.Pass++
			}
			g.AvgValue += s.Value
			g.Partial = g.Partial || (s.Value != 0 && s.Value != 1)
			row.Graders[s.Grader] = g
		}
		walls[i] = append(walls[i], res.Output.Trace.Wall)
		if turns := res.Output.Trace.Turns; len(turns) > 0 && turns[len(turns)-1].EvalDuration > 0 {
			last := turns[len(turns)-1]
			speeds[i] = append(speeds[i], float64(last.EvalCount)/last.EvalDuration.Seconds())
		}
	}

	for i := range rows {
		for name, g := range rows[i].Graders {
			g.AvgValue /= float64(g.Runs)
			rows[i].Graders[name] = g
		}
		w := walls[i]
		slices.Sort(w)
		rows[i].P50 = percentile(w, 0.50)
		rows[i].P95 = percentile(w, 0.95)
		rows[i].TokPerSec = average(speeds[i])
	}
	return rows
}

func WriteTable(w io.Writer, rows []Row) {
	caseOrder := map[string]int{}
	graderSet := map[string]bool{}
	for _, r := range rows {
		if _, ok := caseOrder[r.Case]; !ok {
			caseOrder[r.Case] = len(caseOrder)
		}
		for name := range r.Graders {
			graderSet[name] = true
		}
	}
	graders := slices.Sorted(maps.Keys(graderSet))

	sorted := slices.Clone(rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Case != b.Case {
			return caseOrder[a.Case] < caseOrder[b.Case]
		}
		if a.Pass != b.Pass {
			return a.Pass > b.Pass
		}
		return a.P50 < b.P50
	})

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := append([]string{"TARGET", "CASE", "PASS", "ERR"}, graders...)
	header = append(header, "p50(s)", "p95(s)", "tok/s")
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, r := range sorted {
		cells := []string{r.Target, r.Case, fmt.Sprintf("%d/%d", r.Pass, r.Runs), fmt.Sprint(r.Errors)}
		for _, name := range graders {
			cells = append(cells, graderCell(r, name))
		}
		cells = append(cells,
			fmt.Sprintf("%.1f", r.P50.Seconds()),
			fmt.Sprintf("%.1f", r.P95.Seconds()),
			fmt.Sprintf("%.0f", r.TokPerSec),
		)
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	tw.Flush()
}

func graderCell(r Row, name string) string {
	g, ok := r.Graders[name]
	if !ok {
		return "-"
	}
	cell := fmt.Sprintf("%d/%d", g.Pass, g.Runs)
	if g.Partial {
		cell += fmt.Sprintf(" (%.2f)", g.AvgValue)
	}
	return cell
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(p*float64(len(sorted))+0.5) - 1
	return sorted[max(0, min(i, len(sorted)-1))]
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}
