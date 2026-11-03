package main

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"ai-eval/internal/eval"
	"ai-eval/internal/report"
)

func safeName(name string) string {
	safe := strings.NewReplacer("/", "_", ":", "_").Replace(name)
	if safe == "" || safe == "." || safe == ".." {
		return "_"
	}
	return safe
}

func outputDir(res eval.Result, start time.Time) string {
	return filepath.Join(res.Dir, "outputs", start.Format(report.TimeFormat), safeName(res.Target), safeName(res.Case), fmt.Sprintf("run-%d", res.N))
}

func saveOutput(dir string, res eval.Result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	var errs []error
	if err := root.WriteFile("answer.md", []byte(res.Output.Answer), 0o644); err != nil {
		errs = append(errs, err)
	}
	if res.Err != "" {
		if err := root.WriteFile("error.txt", []byte(res.Err), 0o644); err != nil {
			errs = append(errs, err)
		}
	}

	names := make([]string, 0, len(res.Output.Files))
	for name := range res.Output.Files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := saveWorkFile(root, name, res.Output.Files[name]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func saveWorkFile(root *os.Root, name string, content []byte) error {
	if !filepath.IsLocal(name) {
		return fmt.Errorf("refusing file name %q", name)
	}
	target := path.Join("work", path.Clean(name))
	if err := root.MkdirAll(path.Dir(target), 0o755); err != nil {
		return err
	}
	return root.WriteFile(target, content, 0o644)
}
