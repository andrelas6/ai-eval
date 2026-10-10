package eval

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"ai-eval/internal/grader"
	"ai-eval/internal/tool"
)

const repoRoot = "../.."

func decode(t *testing.T, src string) (Case, error) {
	t.Helper()
	m, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	return decodeCase(m, "case", repoRoot)
}

func graderNames(c Case) []string {
	var names []string
	for _, g := range c.Graders {
		names = append(names, g.Name())
	}
	return names
}

// TestLoadCaseFromTheRepo checks the real evals/list-files.yaml loads into a ready-to-run case.
// It asserts every field is read, runs becomes a number, list_dir is built pointing at the
// fixture, and the three graders are built in order, with files reading the fixture from disk.
func TestLoadCaseFromTheRepo(t *testing.T) {
	c, err := LoadCase(filepath.Join(repoRoot, "evals/list-files.yaml"), repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	if c.ID != "list-files" || c.Prompt != "list the files of this directory" || c.Runs != 5 {
		t.Errorf("case = %+v", c)
	}
	if !strings.Contains(c.System, "list_dir") {
		t.Errorf("system = %q", c.System)
	}
	if c.Fixture != filepath.Join(repoRoot, "fixtures/sample") {
		t.Errorf("fixture = %q", c.Fixture)
	}
	if len(c.Tools) != 1 || c.Tools[0].(tool.ListDir).Root != c.Fixture {
		t.Errorf("tools = %+v", c.Tools)
	}
	if got := graderNames(c); !slices.Equal(got, []string{"files", "tool_called", "latency"}) {
		t.Errorf("graders = %v", got)
	}
	if files := c.Graders[0].(grader.Files); !slices.Contains(files.Expected, "README.md") {
		t.Errorf("files grader expected = %v", files.Expected)
	}
	if tc := c.Graders[1].(grader.ToolCalled); tc.Tool != "list_dir" {
		t.Errorf("tool_called = %+v", tc)
	}
}

// TestLoadCaseIDFromFileName checks a case without an id is named after its file.
// It asserts evals/other.yaml without "id:" gets the id "other".
func TestLoadCaseIDFromFileName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.yaml")
	os.WriteFile(path, []byte("prompt: hi\ngraders: [latency]\n"), 0o644)

	c, err := LoadCase(path, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "other" {
		t.Errorf("id = %q", c.ID)
	}
}

// TestLoadCaseErrorsNameTheFile checks errors point at the file they came from.
// It asserts a missing file and a YAML error both mention the path.
func TestLoadCaseErrorsNameTheFile(t *testing.T) {
	if _, err := LoadCase("nope.yaml", repoRoot); err == nil || !strings.Contains(err.Error(), "nope.yaml") {
		t.Errorf("missing file: err = %v", err)
	}

	path := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(path, []byte("prompt: |\n  hi\n"), 0o644)
	if _, err := LoadCase(path, repoRoot); err == nil || !strings.Contains(err.Error(), "bad.yaml: line 1") {
		t.Errorf("yaml error: err = %v", err)
	}
}

// TestDecodeCaseDefaults checks the smallest valid case: a prompt and one grader.
// It asserts runs defaults to 1, there is no system prompt, fixture or tools, and a
// case that doesn't touch files doesn't need a fixture.
func TestDecodeCaseDefaults(t *testing.T) {
	c, err := decode(t, "prompt: say hi\ngraders: [latency]\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "case" || c.Runs != 1 || c.System != "" || c.Fixture != "" || len(c.Tools) != 0 {
		t.Errorf("case = %+v", c)
	}
}

// TestDecodeCaseLatency checks the two ways to write the latency grader.
// It asserts a bare "latency" has no limit, and "latency: { max: 10s }" reads the duration.
func TestDecodeCaseLatency(t *testing.T) {
	tests := []struct {
		src  string
		want time.Duration
	}{
		{"prompt: hi\ngraders: [latency]\n", 0},
		{"prompt: hi\ngraders:\n  - latency: { max: 10s }\n", 10 * time.Second},
		{"prompt: hi\ngraders:\n  - latency: { max: 1m30s }\n", 90 * time.Second},
	}
	for _, tc := range tests {
		c, err := decode(t, tc.src)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.Graders[0].(grader.Latency).Max; got != tc.want {
			t.Errorf("%q: max = %v, want %v", tc.src, got, tc.want)
		}
	}
}

// TestDecodeCaseErrors checks mistakes in a case file are caught before any model is called.
// Each row asserts the error says what is wrong: missing or misspelled fields, unknown tools
// or graders, bad numbers and durations, a grader checking a tool the case doesn't offer,
// and a fixture that is needed but missing.
func TestDecodeCaseErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"no prompt", "graders: [latency]", `"prompt" is required`},
		{"no graders", "prompt: hi", `"graders" needs at least one grader`},
		{"misspelled field", "promt: hi\nprompt: hi\ngraders: [latency]", `unknown field "promt"`},
		{"runs not a number", "prompt: hi\nruns: five\ngraders: [latency]", `"runs" must be a whole number above 0, got "five"`},
		{"runs zero", "prompt: hi\nruns: 0\ngraders: [latency]", `"runs" must be a whole number above 0, got "0"`},
		{"prompt is a list", "prompt: [a, b]\ngraders: [latency]", `"prompt" must be text`},
		{"tools not a list", "prompt: hi\nfixture: fixtures/sample\ntools: list_dir\ngraders: [latency]", `"tools" must be a list`},
		{"unknown tool", "prompt: hi\nfixture: fixtures/sample\ntools: [rm_rf]\ngraders: [latency]", `unknown tool "rm_rf"`},
		{"unknown grader", "prompt: hi\ngraders: [vibes]", `unknown grader "vibes" (known: files, latency, tool_called)`},
		{"grader written as a list", "prompt: hi\ngraders:\n  - [files]", "each grader must be a name or name: settings"},
		{"bad duration", "prompt: hi\ngraders:\n  - latency: { max: fast }", `latency: "max" must be a duration like 10s, got "fast"`},
		{"latency unknown setting", "prompt: hi\ngraders:\n  - latency: { min: 1s }", `latency: unknown setting "min"`},
		{"tool_called without a tool", "prompt: hi\ngraders: [tool_called]", "tool_called: say which tool, e.g. tool_called: list_dir"},
		{"tool_called for a tool not offered", "prompt: hi\ngraders:\n  - tool_called: list_dir", `tool_called: the case doesn't offer "list_dir" in tools`},
		{"files without fixture", "prompt: hi\ngraders: [files]", `files: needs a "fixture"`},
		{"tools without fixture", "prompt: hi\ntools: [list_dir]\ngraders: [latency]", `tools need a "fixture"`},
		{"fixture missing on disk", "prompt: hi\nfixture: fixtures/nope\ngraders: [latency]", `fixture "fixtures/nope" not found`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decode(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
