// Command ai-eval asks each Ollama model to list a directory via a tool call,
// then scores the answer for correctness and speed.
package main

import (
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
	maxTurns     = 4
)

// Run is one scored attempt of one model.
type Run struct {
	Model     string        `json:"model"`
	N         int           `json:"n"`
	Answer    string        `json:"answer"`
	ToolCalls []string      `json:"tool_calls"`
	Score     Result        `json:"score"`
	Wall      time.Duration `json:"wall_ns"`
	TokPerSec float64       `json:"tok_per_sec"` // final turn only
	Err       string        `json:"error,omitempty"`
}

func main() {
	host := flag.String("host", "http://192.168.2.156:11434", "Ollama base URL")
	runs := flag.Int("runs", 5, "scored runs per model")
	models := flag.String("models", "", "comma-separated models (empty = all)")
	dir := flag.String("dir", "testdata/sample", "fixture directory the model lists")
	timeout := flag.Duration("timeout", 120*time.Second, "timeout per run")
	flag.Parse()

	expected, known, err := fixtureNames(*dir)
	if err != nil {
		log.Fatalf("read fixture: %v", err)
	}

	client := &Ollama{Host: *host, HTTP: &http.Client{}}
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
		log.Printf("%s: warm-up", m)
		warm(client, m, *timeout)
		for i := 1; i <= *runs; i++ {
			r := runOnce(client, m, *dir, *timeout)
			r.N = i
			if r.Err == "" {
				r.Score = Score(r.Answer, expected, known)
			}
			log.Printf("%s run %d: pass=%v recall=%.2f halluc=%d %.1fs %s",
				m, i, r.Score.Pass, r.Score.Recall, r.Score.Hallucinated, r.Wall.Seconds(), r.Err)
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
func warm(c *Ollama, model string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout*3)
	defer cancel()
	if _, err := c.Chat(ctx, ChatRequest{Model: model, Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		log.Printf("%s: warm-up failed: %v", model, err)
	}
}

func runOnce(c *Ollama, model, dir string, timeout time.Duration) (r Run) {
	r.Model = model
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	msgs := []Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}}
	start := time.Now()
	defer func() { r.Wall = time.Since(start) }()

	for turn := 0; turn < maxTurns; turn++ {
		resp, err := c.Chat(ctx, ChatRequest{Model: model, Messages: msgs, Tools: []Tool{listDirTool}})
		if err != nil {
			r.Err = err.Error()
			return r
		}
		msgs = append(msgs, resp.Message)

		if len(resp.Message.ToolCalls) == 0 {
			r.Answer = resp.Message.Content
			if resp.EvalDuration > 0 {
				r.TokPerSec = float64(resp.EvalCount) / (float64(resp.EvalDuration) / 1e9)
			}
			return r
		}

		for _, tc := range resp.Message.ToolCalls {
			path, _ := tc.Function.Arguments["path"].(string)
			r.ToolCalls = append(r.ToolCalls, fmt.Sprintf("%s(%q)", tc.Function.Name, path))
			var out string
			if tc.Function.Name != "list_dir" {
				out = "error: unknown tool " + tc.Function.Name
			} else if out, err = runListDir(dir, path); err != nil {
				out = "error: " + err.Error()
			}
			msgs = append(msgs, Message{Role: "tool", Content: out, ToolName: tc.Function.Name})
		}
	}
	r.Err = fmt.Sprintf("no final answer after %d turns", maxTurns)
	return r
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
			if r.Score.Pass {
				rw.pass++
			}
			if len(r.ToolCalls) > 0 {
				rw.tool++
			}
			rw.recall += r.Score.Recall
			rw.halluc += float64(r.Score.Hallucinated)
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
