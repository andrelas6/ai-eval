// Command ai-eval asks each Ollama model to list a directory via a tool call,
// then scores the answer for correctness and speed.
package main

import (
	"ai-eval/internal/grader"
	"ai-eval/internal/ollama"
	"ai-eval/internal/target"
	"ai-eval/internal/tool"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

const (
	systemPrompt = "You are a helpful assistant with access to a `list_dir` tool. The current directory is `.`."
	userPrompt   = "list the files of this directory"
)

// Run is one scored attempt of one model.
type Run struct {
	Model     string         `json:"model"`
	N         int            `json:"n"`
	Answer    string         `json:"answer"`
	ToolCalls []string       `json:"tool_calls"`
	Pass      bool           `json:"pass"`
	Scores    []grader.Score `json:"scores"`
	Wall      time.Duration  `json:"wall_ns"`
	TokPerSec float64        `json:"tok_per_sec"` // final turn only
	Err       string         `json:"error,omitempty"`
}

func main() {
	host := flag.String("host", "http://192.168.2.156:11434", "Ollama base URL")
	runs := flag.Int("runs", 5, "scored runs per model")
	models := flag.String("models", "", "comma-separated models (empty = all)")
	dir := flag.String("dir", "fixtures/sample", "fixture directory the model lists")
	timeout := flag.Duration("timeout", 120*time.Second, "timeout per run")
	flag.Parse()

	files, err := grader.NewFiles(*dir)
	if err != nil {
		log.Fatalf("read fixture: %v", err)
	}
	graders := []grader.Grader{files, grader.Latency{}}

	client := &ollama.Client{Host: *host, HTTP: &http.Client{}}
	names := splitList(*models)
	if len(names) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		names, err = client.ListModels(ctx)
		cancel()
		if err != nil {
			log.Fatalf("list models: %v", err)
		}
	}

	var all []Run
	for _, m := range names {
		t := target.Ollama{Client: client, Model: m}
		log.Printf("%s: warm-up", m)
		warm(t, *timeout)
		for i := 1; i <= *runs; i++ {
			r := runOnce(t, *dir, *timeout, graders)
			r.N = i
			log.Printf("%s run %d: pass=%v recall=%.2f halluc=%.0f %.1fs %s",
				m, i, r.Pass, r.metric("files", "recall"), r.metric("files", "hallucinated"), r.Wall.Seconds(), r.Err)
			all = append(all, r)
		}
	}

	printTable(all)
	if path, err := writeJSON(all); err != nil {
		log.Printf("write results: %v", err)
	} else {
		fmt.Println("\nfull results:", path)
	}
}

// warm loads the model into memory so load time doesn't skew scored runs.
func warm(t target.Target, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout*3)
	defer cancel()
	if _, err := t.Run(ctx, target.Input{Prompt: "hi"}); err != nil {
		log.Printf("%s: warm-up failed: %v", t.Name(), err)
	}
}

func runOnce(t target.Target, dir string, timeout time.Duration, graders []grader.Grader) Run {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out, err := t.Run(ctx, target.Input{
		System: systemPrompt,
		Prompt: userPrompt,
		Tools:  []tool.Tool{tool.ListDir{Root: dir}},
	})

	r := Run{Model: t.Name(), Answer: out.Answer, Wall: out.Trace.Wall}
	if err != nil {
		r.Err = err.Error()
	} else {
		r.Pass = true
		for _, g := range graders {
			s := g.Grade(out)
			r.Scores = append(r.Scores, s)
			r.Pass = r.Pass && s.Pass
		}
	}
	for _, c := range out.Trace.ToolCalls() {
		path, _ := c.Args["path"].(string)
		r.ToolCalls = append(r.ToolCalls, fmt.Sprintf("%s(%q)", c.Name, path))
	}
	if n := len(out.Trace.Turns); n > 0 && out.Trace.Turns[n-1].EvalDuration > 0 {
		last := out.Trace.Turns[n-1]
		r.TokPerSec = float64(last.EvalCount) / last.EvalDuration.Seconds()
	}
	return r
}

func (r Run) metric(grader, name string) float64 {
	for _, s := range r.Scores {
		if s.Grader == grader {
			return s.Metrics[name]
		}
	}
	return 0
}

func printTable(all []Run) {
	type row struct {
		model                 string
		pass, tool, n         int
		recall, halluc, tokps float64
		p50, p95              time.Duration
	}
	byModel := map[string][]Run{}
	var order []string
	for _, r := range all {
		if _, ok := byModel[r.Model]; !ok {
			order = append(order, r.Model)
		}
		byModel[r.Model] = append(byModel[r.Model], r)
	}

	var rows []row
	for _, m := range order {
		rs := byModel[m]
		rw := row{model: m, n: len(rs)}
		var walls []time.Duration
		for _, r := range rs {
			if r.Pass {
				rw.pass++
			}
			if len(r.ToolCalls) > 0 {
				rw.tool++
			}
			rw.recall += r.metric("files", "recall")
			rw.halluc += r.metric("files", "hallucinated")
			rw.tokps += r.TokPerSec
			walls = append(walls, r.Wall)
		}
		k := float64(len(rs))
		rw.recall, rw.halluc, rw.tokps = rw.recall/k, rw.halluc/k, rw.tokps/k
		sort.Slice(walls, func(i, j int) bool { return walls[i] < walls[j] })
		rw.p50 = percentile(walls, 0.50)
		rw.p95 = percentile(walls, 0.95)
		rows = append(rows, rw)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].pass != rows[j].pass {
			return rows[i].pass > rows[j].pass
		}
		return rows[i].p50 < rows[j].p50
	})

	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tPASS\tRECALL\tHALLUC\tTOOL\tp50(s)\tp95(s)\ttok/s")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%d/%d\t%.2f\t%.1f\t%d/%d\t%.1f\t%.1f\t%.0f\n",
			r.model, r.pass, r.n, r.recall, r.halluc, r.tool, r.n, r.p50.Seconds(), r.p95.Seconds(), r.tokps)
	}
	tw.Flush()
}

// percentile uses nearest-rank on a sorted slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(p*float64(len(sorted))+0.5) - 1
	return sorted[max(0, min(i, len(sorted)-1))]
}

func writeJSON(all []Run) (string, error) {
	if err := os.MkdirAll("results", 0o755); err != nil {
		return "", err
	}
	path := filepath.Join("results", time.Now().Format("20060102-150405")+".json")
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o644)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
