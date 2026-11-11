package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"ai-eval/internal/eval"
	"ai-eval/internal/ollama"
	"ai-eval/internal/report"
	"ai-eval/internal/target"
)

type targetFactory func(agent, model, host string, skillDirs []string) target.Target

type app struct {
	stdout     io.Writer
	stderr     io.Writer
	build      targetFactory
	resultsDir string
	now        func() time.Time
}

type skillSets [][]string

func (s *skillSets) String() string { return fmt.Sprint([][]string(*s)) }

func (s *skillSets) Set(value string) error {
	var names []string
	for _, name := range splitList(value) {
		if name != "none" {
			names = append(names, name)
		}
	}
	*s = append(*s, names)
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	a := app{
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		resultsDir: "results",
		now:        time.Now,
	}
	a.build = realTarget
	if err := a.run(ctx, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func realTarget(agent, model, host string, skillDirs []string) target.Target {
	if agent == "pi" {
		return target.Pi{Bin: "pi", Model: model, Skills: skillDirs}
	}
	return target.Ollama{Client: &ollama.Client{Host: host, HTTP: &http.Client{}}, Model: model}
}

func (a app) run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("aieval", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	agent := flags.String("agent", "ollama", "agent to run: ollama|pi")
	models := flags.String("models", "", "comma-separated models (required)")
	host := flags.String("host", "http://192.168.2.156:11434", "Ollama base URL")
	timeout := flags.Duration("timeout", 120*time.Second, "timeout per try")
	retries := flags.Int("retries", 2, "extra tries after a retryable failure")
	var sets skillSets
	flags.Var(&sets, "skills", "skill set: none, or names joined with commas (repeatable)")
	var minPass float64
	flags.Func("min-pass", "pass rate a grid cell must reach to be picked, 1 to 100, e.g. 90%", func(value string) error {
		rate, err := parseMinPass(value)
		if err != nil {
			return err
		}
		minPass = rate
		return nil
	})
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *agent != "ollama" && *agent != "pi" {
		return fmt.Errorf("-agent must be ollama|pi, got %q", *agent)
	}
	if *retries < 0 {
		return errors.New("-retries must be 0 or more")
	}
	names := splitList(*models)
	if len(names) == 0 {
		return errors.New("-models is required, e.g. -models gpt-oss:20b")
	}
	if *agent == "pi" {
		for _, m := range names {
			if provider, id, ok := strings.Cut(m, "/"); !ok || provider == "" || id == "" {
				return fmt.Errorf("-models value %q must be written provider/model, e.g. ollama/gpt-oss:20b", m)
			}
		}
	}
	if len(sets) > 0 && *agent == "ollama" {
		return errors.New("-skills only works with -agent pi")
	}
	if flags.NArg() == 0 {
		return errors.New("give a suite folder or case files to run")
	}

	files, suite, err := caseFiles(flags.Args())
	if err != nil {
		return err
	}
	dirs, err := skillDirs(sets, suite)
	if err != nil {
		return err
	}

	var cases []eval.Case
	for _, file := range files {
		c, err := eval.LoadCase(file)
		if err != nil {
			return err
		}
		cases = append(cases, c)
	}

	var targets []target.Target
	var axes []report.Axis
	for _, model := range names {
		for i, dir := range dirs {
			built := a.build(*agent, model, *host, dir)
			targets = append(targets, built)
			axes = append(axes, report.Axis{Target: built.Name(), Skills: skillLabel(sets, i), Model: model})
		}
	}

	start := a.now()
	type runKey struct {
		target, name string
		n            int
	}
	saved := map[runKey]string{}
	runner := eval.Runner{
		Timeout: *timeout,
		Retries: *retries,
		Logf: func(format string, args ...any) {
			fmt.Fprintf(a.stderr, format+"\n", args...)
		},
		OnResult: func(r eval.Result) {
			fmt.Fprintf(a.stderr, "%s %s run %d: pass=%v %s\n", r.Target, r.Case, r.N, r.Pass, r.Err)
			if r.Output.Trace.FilesErr != "" {
				fmt.Fprintf(a.stderr, "warning: %s %s run %d: could not read new files: %s\n", r.Target, r.Case, r.N, r.Output.Trace.FilesErr)
			}
			dir := outputDir(r, start)
			if err := saveOutput(dir, r); err != nil {
				fmt.Fprintf(a.stderr, "warning: saving output of %s %s run %d: %v\n", r.Target, r.Case, r.N, err)
				return
			}
			saved[runKey{r.Target, r.Case, r.N}] = dir
		},
	}
	results := runner.Run(ctx, targets, cases)
	for i := range results {
		r := results[i]
		results[i].OutputDir = saved[runKey{r.Target, r.Case, r.N}]
	}

	fmt.Fprintln(a.stdout)
	report.WriteTable(a.stdout, report.Summarize(results))
	grid := report.BuildGrid(results, axes, minPass)
	fmt.Fprintln(a.stdout)
	report.WriteGrid(a.stdout, grid)
	path, err := report.WriteJSON(a.resultsDir, results, grid, start)
	if err != nil {
		return fmt.Errorf("write results: %w", err)
	}
	fmt.Fprintln(a.stdout, "\nfull results:", path)
	return nil
}

func caseFiles(paths []string) (files []string, suite string, err error) {
	suites := map[string]bool{}
	seen := map[string]bool{}
	add := func(file string) {
		key := filepath.Clean(file)
		if !seen[key] {
			seen[key] = true
			files = append(files, file)
		}
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, "", err
		}
		if !info.IsDir() {
			add(p)
			suites[filepath.Clean(filepath.Dir(p))] = true
			continue
		}
		found, err := filepath.Glob(filepath.Join(p, "*.yaml"))
		if err != nil {
			return nil, "", err
		}
		if len(found) == 0 {
			return nil, "", fmt.Errorf("no .yaml cases in %s", p)
		}
		slices.Sort(found)
		for _, file := range found {
			add(file)
		}
		suites[filepath.Clean(p)] = true
	}
	if len(suites) == 1 {
		for dir := range suites {
			suite = dir
		}
	}
	return files, suite, nil
}

func skillDirs(sets skillSets, suite string) ([][]string, error) {
	if len(sets) == 0 {
		return [][]string{nil}, nil
	}
	if suite == "" {
		return nil, errors.New("-skills needs all cases to come from one suite folder")
	}
	var all [][]string
	for _, names := range sets {
		var dirs []string
		for _, name := range names {
			if name == ".." || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
				return nil, fmt.Errorf("skill %q: must be a plain folder name", name)
			}
			dir := filepath.Join(suite, "skills", name)
			file := filepath.Join(dir, "SKILL.md")
			if _, err := os.Stat(file); err != nil {
				return nil, fmt.Errorf("skill %q: no %s", name, file)
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				return nil, err
			}
			resolved, err := filepath.EvalSymlinks(abs)
			if err != nil {
				return nil, err
			}
			dirs = append(dirs, resolved)
		}
		all = append(all, dirs)
	}
	return all, nil
}

func skillLabel(sets skillSets, i int) string {
	if i >= len(sets) || len(sets[i]) == 0 {
		return "none"
	}
	return strings.Join(sets[i], ",")
}

func parseMinPass(value string) (float64, error) {
	number, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(value), "%"), 64)
	if err != nil || !(number >= 1 && number <= 100) {
		return 0, errors.New("must be a number from 1 to 100, e.g. 90% or 90")
	}
	return number / 100, nil
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
