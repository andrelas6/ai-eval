package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-eval/internal/target"
)

type scriptedTarget struct {
	name  string
	run   func(n int) (target.Output, error)
	calls int
}

func (s *scriptedTarget) Name() string { return s.name }

func (s *scriptedTarget) Run(ctx context.Context, in target.Input) (target.Output, error) {
	s.calls++
	return s.run(s.calls)
}

func runWith(t *testing.T, tg target.Target, resultsDir string, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	a := app{
		stdout:     &out,
		stderr:     &errOut,
		build:      func(agent, model, host string, skillDirs []string) target.Target { return tg },
		resultsDir: resultsDir,
		now:        func() time.Time { return time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC) },
	}
	err := a.run(context.Background(), args)
	return out.String(), errOut.String(), err
}

func answerTarget(name, answer string) *scriptedTarget {
	return &scriptedTarget{name: name, run: func(int) (target.Output, error) {
		return target.Output{Answer: answer}, nil
	}}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestRunSavesAnswer checks each run's answer is saved in its suite folder.
// It asserts answer.md under outputs/<timestamp>/<target>/<case>/run-1 holds the answer and no error.txt exists.
func TestRunSavesAnswer(t *testing.T) {
	suite := writeSuite(t)
	_, _, err := runWith(t, answerTarget("fake@m", "the answer"), t.TempDir(), "-models", "m", suite)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(suite, "outputs", "20261103-100000", "fake@m", "a", "run-1")
	if got := readFile(t, filepath.Join(dir, "answer.md")); got != "the answer" {
		t.Errorf("answer.md = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "error.txt")); !os.IsNotExist(err) {
		t.Errorf("error.txt err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "work")); !os.IsNotExist(err) {
		t.Errorf("work err = %v", err)
	}
}

// TestRunSavesErrorText checks an errored run is saved with its error.
// It asserts error.txt holds the error and answer.md still exists.
func TestRunSavesErrorText(t *testing.T) {
	suite := writeSuite(t)
	tg := &scriptedTarget{name: "fake@m", run: func(int) (target.Output, error) {
		return target.Output{Answer: "partial"}, errors.New("model fell over")
	}}
	if _, _, err := runWith(t, tg, t.TempDir(), "-models", "m", suite); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(suite, "outputs", "20261103-100000", "fake@m", "a", "run-1")
	if got := readFile(t, filepath.Join(dir, "error.txt")); got != "model fell over" {
		t.Errorf("error.txt = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "answer.md")); got != "partial" {
		t.Errorf("answer.md = %q", got)
	}
}

// TestRunUsesSafeTargetFolder checks slashes and colons in target names are replaced.
// It asserts pi@openrouter/google/gemini-3.8-flash saves under pi@openrouter_google_gemini-3.8-flash.
func TestRunUsesSafeTargetFolder(t *testing.T) {
	suite := writeSuite(t)
	tg := answerTarget("pi@openrouter/google/gemini-3.8-flash", "x")
	if _, _, err := runWith(t, tg, t.TempDir(), "-models", "m", suite); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(suite, "outputs", "20261103-100000", "pi@openrouter_google_gemini-3.8-flash", "a", "run-1", "answer.md")
	readFile(t, path)
}

// TestSafeNameReplacesColonsAndSlashes checks the folder name helper.
// It asserts colons and slashes become underscores and dot-only names are replaced.
func TestSafeNameReplacesColonsAndSlashes(t *testing.T) {
	for in, want := range map[string]string{"ollama:gpt-oss:20b": "ollama_gpt-oss_20b", "a/b": "a_b", "../x": ".._x", "..": "_", ".": "_", "": "_"} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRunKeepsCaseIdInsideOutputsFolder checks a case id can't escape or nest.
// It asserts ids like ../x and a/b are saved in single folders under the target folder.
func TestRunKeepsCaseIdInsideOutputsFolder(t *testing.T) {
	suite := writeSuite(t)
	writeFile(t, filepath.Join(suite, "a.yaml"), "id: ../x\nprompt: first\ngraders: [latency]\n")
	writeFile(t, filepath.Join(suite, "b.yaml"), "id: a/b\nprompt: second\ngraders: [latency]\n")
	if _, _, err := runWith(t, answerTarget("fake@m", "x"), t.TempDir(), "-models", "m", suite); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(suite, "outputs", "20261103-100000", "fake@m")
	readFile(t, filepath.Join(base, ".._x", "run-1", "answer.md"))
	readFile(t, filepath.Join(base, "a_b", "run-1", "answer.md"))
}

// TestRunSharesOneTimestamp checks the JSON file and the outputs folder use the same start time.
// It asserts the clock is read once, so a later reading doesn't change either name.
func TestRunSharesOneTimestamp(t *testing.T) {
	suite := writeSuite(t)
	resultsDir := t.TempDir()
	var out, errOut bytes.Buffer
	reads := 0
	a := app{
		stdout:     &out,
		stderr:     &errOut,
		build:      func(agent, model, host string, skillDirs []string) target.Target { return answerTarget("fake@m", "x") },
		resultsDir: resultsDir,
		now: func() time.Time {
			reads++
			return time.Date(2026, 11, 3, 10, 0, reads, 0, time.UTC)
		},
	}
	if err := a.run(context.Background(), []string{"-models", "m", suite}); err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Errorf("clock read %d times", reads)
	}
	readFile(t, filepath.Join(resultsDir, "20261103-100001.json"))
	readFile(t, filepath.Join(suite, "outputs", "20261103-100001", "fake@m", "a", "run-1", "answer.md"))
}

// TestRunRecordsOutputDirInJSON checks the results file points at each run's folder.
// It asserts every result has the suite folder and the output_dir of its run.
func TestRunRecordsOutputDirInJSON(t *testing.T) {
	suite := writeSuite(t)
	resultsDir := t.TempDir()
	if _, _, err := runWith(t, answerTarget("fake@m", "x"), resultsDir, "-models", "m", suite); err != nil {
		t.Fatal(err)
	}
	var file struct {
		Results []struct {
			Case      string `json:"case"`
			Suite     string `json:"suite"`
			OutputDir string `json:"output_dir"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(resultsDir, "20261103-100000.json"))), &file); err != nil {
		t.Fatal(err)
	}
	results := file.Results
	if len(results) != 2 {
		t.Fatalf("results = %v", results)
	}
	for _, r := range results {
		want := filepath.Join(suite, "outputs", "20261103-100000", "fake@m", r.Case, "run-1")
		if r.Suite != suite || r.OutputDir != want {
			t.Errorf("%s: suite = %q, output_dir = %q", r.Case, r.Suite, r.OutputDir)
		}
	}
}

// TestRunSavesEachRunInItsOwnFolder checks a case with runs: 2 keeps both answers.
// It asserts run-1 and run-2 hold different answers.
func TestRunSavesEachRunInItsOwnFolder(t *testing.T) {
	suite := writeSuite(t)
	writeFile(t, filepath.Join(suite, "a.yaml"), "prompt: first\nruns: 2\ngraders: [latency]\n")
	tg := &scriptedTarget{name: "fake@m", run: func(n int) (target.Output, error) {
		return target.Output{Answer: "answer " + string(rune('0'+n))}, nil
	}}
	if _, _, err := runWith(t, tg, t.TempDir(), "-models", "m", filepath.Join(suite, "a.yaml")); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(suite, "outputs", "20261103-100000", "fake@m", "a")
	if got := readFile(t, filepath.Join(base, "run-1", "answer.md")); got != "answer 1" {
		t.Errorf("run-1 = %q", got)
	}
	if got := readFile(t, filepath.Join(base, "run-2", "answer.md")); got != "answer 2" {
		t.Errorf("run-2 = %q", got)
	}
}

// TestRunSavesIntoEachSuiteFolder checks cases from two suites don't share outputs.
// It asserts each suite folder gets only its own case's output.
func TestRunSavesIntoEachSuiteFolder(t *testing.T) {
	root := t.TempDir()
	one := filepath.Join(root, "one")
	two := filepath.Join(root, "two")
	writeFile(t, filepath.Join(one, "alpha.yaml"), "prompt: p\ngraders: [latency]\n")
	writeFile(t, filepath.Join(two, "beta.yaml"), "prompt: p\ngraders: [latency]\n")
	if _, _, err := runWith(t, answerTarget("fake@m", "x"), t.TempDir(), "-models", "m", one, two); err != nil {
		t.Fatal(err)
	}
	stamp := filepath.Join("outputs", "20261103-100000", "fake@m")
	readFile(t, filepath.Join(one, stamp, "alpha", "run-1", "answer.md"))
	readFile(t, filepath.Join(two, stamp, "beta", "run-1", "answer.md"))
	if _, err := os.Stat(filepath.Join(one, stamp, "beta")); !os.IsNotExist(err) {
		t.Errorf("beta in suite one: %v", err)
	}
	if _, err := os.Stat(filepath.Join(two, stamp, "alpha")); !os.IsNotExist(err) {
		t.Errorf("alpha in suite two: %v", err)
	}
}

// TestRunSavesFilesUnderWork checks files from the target land in work/ with their folders.
// It asserts a top-level file and a nested file are written with their bytes.
func TestRunSavesFilesUnderWork(t *testing.T) {
	suite := writeSuite(t)
	tg := &scriptedTarget{name: "fake@m", run: func(int) (target.Output, error) {
		return target.Output{Answer: "x", Files: map[string][]byte{
			"summary.csv":   []byte("a,b"),
			"out/table.md":  []byte("| t |"),
			"out/deep/x.md": []byte("deep"),
		}}, nil
	}}
	if _, _, err := runWith(t, tg, t.TempDir(), "-models", "m", filepath.Join(suite, "a.yaml")); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(suite, "outputs", "20261103-100000", "fake@m", "a", "run-1", "work")
	for name, want := range map[string]string{"summary.csv": "a,b", "out/table.md": "| t |", "out/deep/x.md": "deep"} {
		if got := readFile(t, filepath.Join(work, filepath.FromSlash(name))); got != want {
			t.Errorf("%s = %q", name, got)
		}
	}
}

// TestRunRefusesEscapingFileName checks a file name that climbs out of the run folder is not written.
// It asserts ../x is refused, nothing lands outside the run folder, and the other files are still saved.
func TestRunRefusesEscapingFileName(t *testing.T) {
	suite := writeSuite(t)
	tg := &scriptedTarget{name: "fake@m", run: func(int) (target.Output, error) {
		return target.Output{Answer: "x", Files: map[string][]byte{
			"../x":      []byte("bad"),
			"../../y":   []byte("bad"),
			"/abs.txt":  []byte("bad"),
			"good.md":   []byte("good"),
			"out/ok.md": []byte("ok"),
		}}, nil
	}}
	_, stderr, err := runWith(t, tg, t.TempDir(), "-models", "m", filepath.Join(suite, "a.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(suite, "outputs", "20261103-100000", "fake@m", "a", "run-1")
	work := filepath.Join(run, "work")
	if got := readFile(t, filepath.Join(work, "good.md")); got != "good" {
		t.Errorf("good.md = %q", got)
	}
	if got := readFile(t, filepath.Join(work, "out", "ok.md")); got != "ok" {
		t.Errorf("ok.md = %q", got)
	}
	for _, escaped := range []string{filepath.Join(run, "x"), filepath.Join(run, "..", "y"), filepath.Join(work, "abs.txt")} {
		if _, err := os.Stat(escaped); !os.IsNotExist(err) {
			t.Errorf("%s exists (err = %v)", escaped, err)
		}
	}
	if !strings.Contains(stderr, "warning") {
		t.Errorf("stderr = %q", stderr)
	}
}

// TestRunContinuesWhenSaveFails checks a failed save doesn't stop the eval.
// It asserts a blocked outputs folder gives a warning on stderr, both cases still run, and the JSON is written with empty output_dir.
func TestRunContinuesWhenSaveFails(t *testing.T) {
	suite := writeSuite(t)
	writeFile(t, filepath.Join(suite, "outputs"), "a file, not a folder")
	resultsDir := t.TempDir()
	tg := answerTarget("fake@m", "x")
	stdout, stderr, err := runWith(t, tg, resultsDir, "-models", "m", suite)
	if err != nil {
		t.Fatal(err)
	}
	if tg.calls != 2 {
		t.Errorf("calls = %d", tg.calls)
	}
	if !strings.Contains(stderr, "warning") {
		t.Errorf("stderr = %q", stderr)
	}
	var file struct {
		Results []struct {
			OutputDir string `json:"output_dir"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(resultsDir, "20261103-100000.json"))), &file); err != nil {
		t.Fatal(err)
	}
	for _, r := range file.Results {
		if r.OutputDir != "" {
			t.Errorf("output_dir = %q", r.OutputDir)
		}
	}
	if !strings.Contains(stdout, "full results") {
		t.Errorf("stdout = %q", stdout)
	}
}

// TestRunWarnsWhenNewFilesCouldNotBeRead checks a files error from the target is shown.
// It asserts stderr names the target, case and run, and the eval still saves the answer.
func TestRunWarnsWhenNewFilesCouldNotBeRead(t *testing.T) {
	suite := writeSuite(t)
	tg := &scriptedTarget{name: "fake@m", run: func(int) (target.Output, error) {
		return target.Output{Answer: "x", Trace: target.Trace{FilesErr: "walk failed"}}, nil
	}}
	_, stderr, err := runWith(t, tg, t.TempDir(), "-models", "m", filepath.Join(suite, "a.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "warning: fake@m a run 1: could not read new files: walk failed"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q", stderr)
	}
	readFile(t, filepath.Join(suite, "outputs", "20261103-100000", "fake@m", "a", "run-1", "answer.md"))
}
