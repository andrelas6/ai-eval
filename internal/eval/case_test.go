package eval

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"ai-eval/internal/grader"
	"ai-eval/internal/tool"
)

const suiteRoot = "../../evals/list-files"

func decode(t *testing.T, src string) (Case, error) {
	t.Helper()
	m, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	return decodeCase(m, "case", suiteRoot)
}

func graderNames(c Case) []string {
	var names []string
	for _, g := range c.Graders {
		names = append(names, g.Name())
	}
	return names
}

// TestLoadCaseFromTheRepo checks the real evals/list-files/list-files.yaml loads into a ready-to-run case.
// It asserts every field is read, runs becomes a number, list_dir is built pointing at the
// fixture, and the three graders are built in order, with files reading the fixture from disk.
func TestLoadCaseFromTheRepo(t *testing.T) {
	c, err := LoadCase(filepath.Join(suiteRoot, "list-files.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if c.ID != "list-files" || c.Prompt != "list the files of this directory" || c.Runs != 5 {
		t.Errorf("case = %+v", c)
	}
	if !strings.Contains(c.System, "list_dir") {
		t.Errorf("system = %q", c.System)
	}
	if c.Fixture != filepath.Join(suiteRoot, "fixture") {
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

	c, err := LoadCase(path)
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
	if _, err := LoadCase("nope.yaml"); err == nil || !strings.Contains(err.Error(), "nope.yaml") {
		t.Errorf("missing file: err = %v", err)
	}

	path := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(path, []byte("prompt: |\n  hi\n"), 0o644)
	if _, err := LoadCase(path); err == nil || !strings.Contains(err.Error(), "bad.yaml: line 1") {
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
		{"tools not a list", "prompt: hi\nfixture: fixture\ntools: list_dir\ngraders: [latency]", `"tools" must be a list`},
		{"unknown tool", "prompt: hi\nfixture: fixture\ntools: [rm_rf]\ngraders: [latency]", `unknown tool "rm_rf"`},
		{"unknown grader", "prompt: hi\ngraders: [vibes]", `unknown grader "vibes" (known: contains, files, latency, not_contains, number, regex, tool_called)`},
		{"grader written as a list", "prompt: hi\ngraders:\n  - [files]", "each grader must be a name or name: settings"},
		{"bad duration", "prompt: hi\ngraders:\n  - latency: { max: fast }", `latency: "max" must be a duration like 10s, got "fast"`},
		{"latency unknown setting", "prompt: hi\ngraders:\n  - latency: { min: 1s }", `latency: unknown setting "min"`},
		{"contains unknown setting", "prompt: hi\ngraders:\n  - contains: { text: [a] }", `contains: unknown setting "text"`},
		{"contains empty list", "prompt: hi\ngraders:\n  - contains: []", "contains: give at least one text"},
		{"contains without text", "prompt: hi\ngraders: [contains]", "contains: give at least one text"},
		{"not_contains empty text", "prompt: hi\ngraders:\n  - not_contains: ['']", "not_contains: give at least one text"},
		{"not_contains list of lists", "prompt: hi\ngraders:\n  - not_contains: [[a]]", "not_contains: must be text or a list of text"},
		{"contains blank item", "prompt: hi\ngraders:\n  - contains: [a, '  ']", "contains: give at least one text"},
		{"contains empty item in a list", "prompt: hi\ngraders:\n  - contains: [a, '']", "contains: give at least one text"},
		{"regex given a list", "prompt: hi\ngraders:\n  - regex: [a]", "regex: takes one pattern, not a list"},
		{"number without settings", "prompt: hi\ngraders: [number]", "number: give settings"},
		{"number as a list", "prompt: hi\ngraders:\n  - number: [1]", "number: give settings"},
		{"number unknown setting", "prompt: hi\ngraders:\n  - number: {values: [1], tolerance: 1%}", `number: unknown setting "tolerance"`},
		{"number without values", "prompt: hi\ngraders:\n  - number: {within: 1%}", `number: "values" needs at least one number`},
		{"number empty values", "prompt: hi\ngraders:\n  - number: {values: []}", `number: "values" needs at least one number`},
		{"number values not a list", "prompt: hi\ngraders:\n  - number: {values: 5}", `number: "values" must be a list`},
		{"number value with no number", "prompt: hi\ngraders:\n  - number: {values: [abc]}", `number: value "abc" must hold exactly one number`},
		{"number value with two numbers", "prompt: hi\ngraders:\n  - number: {values: [1 2]}", `number: value "1 2" must hold exactly one number`},
		{"number value is a list", "prompt: hi\ngraders:\n  - number: {values: [[1]]}", `number: "values" must be a list of text`},
		{"number within zero", "prompt: hi\ngraders:\n  - number: {values: [1], within: 0%}", `number: "within" must be a percent above 0 like 1%, got "0%"`},
		{"number within negative", "prompt: hi\ngraders:\n  - number: {values: [1], within: -1%}", `number: "within" must be a percent above 0 like 1%, got "-1%"`},
		{"number within no percent sign", "prompt: hi\ngraders:\n  - number: {values: [1], within: 0.01}", `number: "within" must be a percent above 0 like 1%, got "0.01"`},
		{"number within not finite", "prompt: hi\ngraders:\n  - number: {values: [1], within: Inf%}", `number: "within" must be a percent above 0 like 1%, got "Inf%"`},
		{"number within NaN", "prompt: hi\ngraders:\n  - number: {values: [1], within: NaN%}", `number: "within" must be a percent above 0 like 1%, got "NaN%"`},
		{"number within a list", "prompt: hi\ngraders:\n  - number: {values: [1], within: [1%]}", `number: "within" must be text like 1%`},
		{"number within too big", "prompt: hi\ngraders:\n  - number: {values: [1], within: 1e999%}", `number: "within" must be a percent above 0 like 1%`},
		{"regex without pattern", "prompt: hi\ngraders: [regex]", "regex: give a pattern"},
		{"regex bad pattern", "prompt: hi\ngraders:\n  - regex: '('", "regex: bad pattern"},
		{"regex settings map", "prompt: hi\ngraders:\n  - regex: { pattern: a }", `regex: unknown setting "pattern"`},
		{"grader listed twice", "prompt: hi\ngraders:\n  - contains: [a]\n  - contains: [b]", `grader "contains" is listed twice; list all the values in one contains`},
		{"tool_called without a tool", "prompt: hi\ngraders: [tool_called]", "tool_called: say which tool, e.g. tool_called: list_dir"},
		{"tool_called for a tool not offered", "prompt: hi\ngraders:\n  - tool_called: list_dir", `tool_called: the case doesn't offer "list_dir" in tools`},
		{"files without fixture", "prompt: hi\ngraders: [files]", `files: needs a "fixture"`},
		{"tools without fixture", "prompt: hi\ntools: [list_dir]\ngraders: [latency]", `tools need a "fixture"`},
		{"fixture is a file", "prompt: hi\nfixture: list-files.yaml\ngraders: [latency]", `fixture "../../evals/list-files/list-files.yaml" not found`},
		{"fixture missing on disk", "prompt: hi\nfixture: nope\ngraders: [latency]", `fixture "../../evals/list-files/nope" not found`},
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

// TestLoadCaseRejectsAnEmptyFixture checks a fixture folder with nothing to work on.
// It asserts a folder that holds only dotfiles gives fixture "<resolved path>" is empty, and
// adding a real file makes it load.
func TestLoadCaseRejectsAnEmptyFixture(t *testing.T) {
	dir := t.TempDir()
	pdfs := filepath.Join(dir, "pdfs")
	os.Mkdir(pdfs, 0o755)
	os.WriteFile(filepath.Join(pdfs, ".gitkeep"), nil, 0o644)
	path := filepath.Join(dir, "c.yaml")
	os.WriteFile(path, []byte("prompt: hi\nfixture: pdfs\ngraders: [latency]\n"), 0o644)

	_, err := LoadCase(path)
	if err == nil || !strings.Contains(err.Error(), `fixture "`+pdfs+`" is empty`) {
		t.Fatalf("err = %v", err)
	}

	os.WriteFile(filepath.Join(pdfs, "report.pdf"), []byte("x"), 0o644)
	if _, err := LoadCase(path); err != nil {
		t.Fatal(err)
	}
}

// TestFinancialReportCasesLoad checks the four shipped financial-report cases.
// It asserts each file loads from a temp copy of the suite that has a dummy PDF, so the test
// doesn't need the real PDFs, with fixture pdfs, one run, the latency grader, a not_contains
// grader holding exactly the three refusal phrases, a number grader holding exactly the confirmed
// figures at the default 1%, and a prompt that differs only in the metric. The three money cases
// must also ask for every amount with its unit, since a bare table figure reads as a plain number.
func TestFinancialReportCasesLoad(t *testing.T) {
	suite := t.TempDir()
	os.Mkdir(filepath.Join(suite, "pdfs"), 0o755)
	os.WriteFile(filepath.Join(suite, "pdfs", "report-2024.pdf"), []byte("x"), 0o644)

	ids := map[string]string{
		"revenue.yaml":          "revenue",
		"operating-margin.yaml": "operating margin",
		"net-profit.yaml":       "net profit",
		"debt.yaml":             "total term debt",
	}
	figures := map[string][]string{
		"revenue.yaml":          {"416161 million", "391035 million", "383285 million"},
		"operating-margin.yaml": {"31.97%", "31.51%", "29.82%"},
		"net-profit.yaml":       {"112010 million", "93736 million", "96995 million"},
		"debt.yaml":             {"90678 million", "96662 million"},
	}
	for file, metric := range ids {
		b, err := os.ReadFile(filepath.Join("../../evals/financial-report", file))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(suite, file)
		os.WriteFile(path, b, 0o644)

		c, err := LoadCase(path)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if c.ID != strings.TrimSuffix(file, ".yaml") || c.Runs != 1 || filepath.Base(c.Fixture) != "pdfs" {
			t.Errorf("%s: case = %+v", file, c)
		}
		if got := graderNames(c); !slices.Equal(got, []string{"latency", "not_contains", "number"}) {
			t.Errorf("%s: graders = %v", file, got)
		}
		refusals := grader.Contains{Items: []string{"unable to read", "can't access", "cannot access"}, Absent: true}
		if len(c.Graders) > 1 && !reflect.DeepEqual(c.Graders[1], refusals) {
			t.Errorf("%s: not_contains = %+v", file, c.Graders[1])
		}
		if len(c.Graders) > 2 {
			check, ok := c.Graders[2].(grader.NumberCheck)
			if !ok {
				t.Fatalf("%s: third grader = %T", file, c.Graders[2])
			}
			var written []string
			for _, e := range check.Expected {
				written = append(written, e.Text)
			}
			if !slices.Equal(written, figures[file]) || check.Within != 0.01 {
				t.Errorf("%s: number = %+v", file, check)
			}
		}
		if !strings.Contains(c.Prompt, "How has "+metric+" changed year over year?") {
			t.Errorf("%s: prompt = %q", file, c.Prompt)
		}
		asksForUnits := strings.Contains(c.Prompt, "Write every amount with its unit")
		if asksForUnits != (file != "operating-margin.yaml") {
			t.Errorf("%s: asks for units = %v, prompt = %q", file, asksForUnits, c.Prompt)
		}
	}
}

// TestLoadCaseReportsAnUnreadableFixture checks a fixture folder that can't be read.
// It asserts the read error is reported with the path instead of saying the folder is empty.
func TestLoadCaseReportsAnUnreadableFixture(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any folder")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	os.Mkdir(locked, 0o755)
	os.WriteFile(filepath.Join(locked, "a.pdf"), []byte("x"), 0o644)
	os.Chmod(locked, 0)
	defer os.Chmod(locked, 0o755)
	path := filepath.Join(dir, "c.yaml")
	os.WriteFile(path, []byte("prompt: hi\nfixture: locked\ngraders: [latency]\n"), 0o644)

	_, err := LoadCase(path)

	if err == nil || strings.Contains(err.Error(), "is empty") || !strings.Contains(err.Error(), `fixture "`+locked+`": `) {
		t.Fatalf("err = %v", err)
	}
}

// TestDecodeCaseTextChecks checks the text graders load with their settings.
// It asserts a list of mixed quoting and a single string give the right items, not_contains
// is marked absent, and the regex is compiled.
func TestDecodeCaseTextChecks(t *testing.T) {
	c, err := decode(t, "prompt: hi\ngraders:\n  - contains: [\"2025\", 2024]\n  - not_contains: \"can't access\"\n  - regex: '(?i)total'\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []grader.Grader{
		grader.Contains{Items: []string{"2025", "2024"}},
		grader.Contains{Items: []string{"can't access"}, Absent: true},
	}
	if !slices.EqualFunc(want, c.Graders[:2], func(a, b grader.Grader) bool { return reflect.DeepEqual(a, b) }) {
		t.Errorf("graders = %+v", c.Graders[:2])
	}
	if re := c.Graders[2].(grader.Regex).Pattern.String(); re != "(?i)total" {
		t.Errorf("pattern = %q", re)
	}
}

// TestDecodeCaseNumberCheck checks the number grader loads its settings.
// It asserts each value is read with its scale and kind and keeps its text as written, and
// within defaults to 1% and reads other percents.
func TestDecodeCaseNumberCheck(t *testing.T) {
	c, err := decode(t, "prompt: hi\ngraders:\n  - number: {values: [416161 million, 31.97%]}\n")
	if err != nil {
		t.Fatal(err)
	}
	want := grader.NumberCheck{
		Expected: []grader.ExpectedNumber{
			{Text: "416161 million", Number: grader.Number{Value: 416161e6, Kind: grader.PlainNumber}},
			{Text: "31.97%", Number: grader.Number{Value: 31.97, Kind: grader.PercentNumber}},
		},
		Within: 0.01,
	}
	if !reflect.DeepEqual(c.Graders[0], want) {
		t.Errorf("got %+v", c.Graders[0])
	}

	c, err = decode(t, "prompt: hi\ngraders:\n  - number: {values: [5], within: 0.5%}\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Graders[0].(grader.NumberCheck).Within; got != 0.005 {
		t.Errorf("within = %v", got)
	}
}

// TestDecodeCaseRegexBackslashes checks how backslashes in a regex survive the YAML parser.
// It asserts single quotes keep them as written, so '\|\s*2025\s*\|' compiles to that exact
// pattern and matches a table row, while double quotes drop the backslash and change the pattern.
func TestDecodeCaseRegexBackslashes(t *testing.T) {
	c, err := decode(t, "prompt: hi\ngraders:\n  - regex: '\\|\\s*2025\\s*\\|'\n")
	if err != nil {
		t.Fatal(err)
	}
	re := c.Graders[0].(grader.Regex).Pattern
	if re.String() != `\|\s*2025\s*\|` || !re.MatchString("| 2025 |") {
		t.Errorf("pattern = %q", re.String())
	}

	c, err = decode(t, "prompt: hi\ngraders:\n  - regex: \"\\|\\s*2025\\s*\\|\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Graders[0].(grader.Regex).Pattern.String(); got != "|s*2025s*|" {
		t.Errorf("double-quoted pattern = %q", got)
	}
}
