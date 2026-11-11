package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"ai-eval/internal/target"
)

type fakeTarget struct {
	name string
}

func (f fakeTarget) Name() string { return f.name }

func (f fakeTarget) Run(ctx context.Context, in target.Input) (target.Output, error) {
	return target.Output{Answer: "done", Trace: target.Trace{Wall: time.Millisecond}}, nil
}

type factoryCall struct {
	agent  string
	model  string
	skills []string
}

type fakeFactory struct {
	calls []factoryCall
}

func (f *fakeFactory) build(agent, model, host string, skillDirs []string) target.Target {
	f.calls = append(f.calls, factoryCall{agent, model, skillDirs})
	name := agent + "@" + model
	if len(skillDirs) > 0 {
		names := make([]string, len(skillDirs))
		for i, d := range skillDirs {
			names[i] = filepath.Base(d)
		}
		name = agent + "+" + strings.Join(names, ",") + "@" + model
	}
	return fakeTarget{name: name}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSuite(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "suite")
	writeFile(t, filepath.Join(dir, "b.yaml"), "prompt: second\ngraders: [latency]\n")
	writeFile(t, filepath.Join(dir, "a.yaml"), "prompt: first\ngraders: [latency]\n")
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a case")
	writeFile(t, filepath.Join(dir, "skills", "x", "SKILL.md"), "x")
	writeFile(t, filepath.Join(dir, "skills", "y", "SKILL.md"), "y")
	return dir
}

func execute(t *testing.T, factory *fakeFactory, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	app := app{
		stdout:     &out,
		stderr:     &errOut,
		build:      factory.build,
		resultsDir: filepath.Join(t.TempDir(), "results"),
		now:        func() time.Time { return time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC) },
	}
	err = app.run(context.Background(), args)
	return out.String(), errOut.String(), err
}

// TestRunNeedsModels checks the -models flag is required.
// It asserts running without it fails with a message that names the flag.
func TestRunNeedsModels(t *testing.T) {
	_, _, err := execute(t, &fakeFactory{}, "-agent", "ollama", writeSuite(t))
	if err == nil || !strings.Contains(err.Error(), "-models") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunNeedsASuite checks a suite folder or case file is required.
// It asserts running with only -models fails with a message.
func TestRunNeedsASuite(t *testing.T) {
	_, _, err := execute(t, &fakeFactory{}, "-models", "a")
	if err == nil || !strings.Contains(err.Error(), "suite") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunRejectsUnknownAgent checks -agent only takes ollama or pi.
// It asserts another value fails and names the allowed ones.
func TestRunRejectsUnknownAgent(t *testing.T) {
	_, _, err := execute(t, &fakeFactory{}, "-agent", "claude", "-models", "a", writeSuite(t))
	if err == nil || !strings.Contains(err.Error(), "ollama|pi") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunRejectsSkillsWithOllama checks skill sets only work with the pi agent.
// It asserts -skills with -agent ollama fails before any target is built.
func TestRunRejectsSkillsWithOllama(t *testing.T) {
	factory := &fakeFactory{}
	_, _, err := execute(t, factory, "-agent", "ollama", "-models", "a", "-skills", "x", writeSuite(t))
	if err == nil || !strings.Contains(err.Error(), "-skills") {
		t.Fatalf("err = %v", err)
	}
	if len(factory.calls) != 0 {
		t.Errorf("calls = %v", factory.calls)
	}
}

// TestRunFailsWhenSkillFileIsMissing checks skills are checked before anything runs.
// It asserts a skill name with no SKILL.md fails with the missing path, and no target is built.
func TestRunFailsWhenSkillFileIsMissing(t *testing.T) {
	factory := &fakeFactory{}
	suite := writeSuite(t)
	_, _, err := execute(t, factory, "-agent", "pi", "-models", "p/a", "-skills", "x,ghost", suite)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(suite, "skills", "ghost", "SKILL.md")) {
		t.Fatalf("err = %v", err)
	}
	if len(factory.calls) != 0 {
		t.Errorf("calls = %v", factory.calls)
	}
}

// TestRunBuildsEveryModelAndSkillSet checks the target matrix.
// It asserts two models and the skill sets none and x build four targets, in model order
// then skill set order, and that skill names become SKILL.md folders inside the suite.
func TestRunBuildsEveryModelAndSkillSet(t *testing.T) {
	factory := &fakeFactory{}
	suite := writeSuite(t)
	stdout, _, err := execute(t, factory, "-agent", "pi", "-models", "p/a,p/b", "-skills", "none", "-skills", "x", suite)
	if err != nil {
		t.Fatal(err)
	}

	want := []factoryCall{
		{"pi", "p/a", nil},
		{"pi", "p/a", []string{filepath.Join(suite, "skills", "x")}},
		{"pi", "p/b", nil},
		{"pi", "p/b", []string{filepath.Join(suite, "skills", "x")}},
	}
	if len(factory.calls) != len(want) {
		t.Fatalf("calls = %v", factory.calls)
	}
	for i, c := range factory.calls {
		if c.agent != want[i].agent || c.model != want[i].model || !slices.Equal(c.skills, want[i].skills) {
			t.Errorf("call %d = %+v, want %+v", i, c, want[i])
		}
	}
	for _, name := range []string{"pi@p/a", "pi+x@p/a", "pi@p/b", "pi+x@p/b"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("table is missing %q:\n%s", name, stdout)
		}
	}
}

// TestRunJoinsSkillNamesWithCommas checks one skill set can hold several skills.
// It asserts -skills x,y builds one target that gets both skill folders in order.
func TestRunJoinsSkillNamesWithCommas(t *testing.T) {
	factory := &fakeFactory{}
	suite := writeSuite(t)
	if _, _, err := execute(t, factory, "-agent", "pi", "-models", "p/a", "-skills", "x,y", suite); err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(suite, "skills", "x"), filepath.Join(suite, "skills", "y")}
	if len(factory.calls) != 1 || !slices.Equal(factory.calls[0].skills, want) {
		t.Fatalf("calls = %v", factory.calls)
	}
}

// TestRunWithoutSkillsBuildsOneTargetPerModel checks the default skill set.
// It asserts no -skills flag means one target per model with no skills.
func TestRunWithoutSkillsBuildsOneTargetPerModel(t *testing.T) {
	factory := &fakeFactory{}
	if _, _, err := execute(t, factory, "-agent", "ollama", "-models", "a,b", writeSuite(t)); err != nil {
		t.Fatal(err)
	}
	if len(factory.calls) != 2 || factory.calls[0].skills != nil || factory.calls[1].model != "b" {
		t.Fatalf("calls = %v", factory.calls)
	}
}

// TestRunLoadsSuiteFolderInNameOrder checks a folder loads every yaml file in it.
// It asserts a.yaml comes before b.yaml in the table and notes.txt is ignored.
func TestRunLoadsSuiteFolderInNameOrder(t *testing.T) {
	stdout, _, err := execute(t, &fakeFactory{}, "-agent", "ollama", "-models", "m", writeSuite(t))
	if err != nil {
		t.Fatal(err)
	}
	a, b := strings.Index(stdout, "  a  "), strings.Index(stdout, "  b  ")
	if a < 0 || b < 0 || a > b {
		t.Fatalf("table:\n%s", stdout)
	}
	if strings.Contains(stdout, "notes") {
		t.Errorf("table:\n%s", stdout)
	}
}

// TestRunAcceptsCaseFiles checks single case files can be given instead of a folder.
// It asserts only the named case runs and the skills still come from that file's folder.
func TestRunAcceptsCaseFiles(t *testing.T) {
	factory := &fakeFactory{}
	suite := writeSuite(t)
	stdout, _, err := execute(t, factory, "-agent", "pi", "-models", "p/m", "-skills", "x", filepath.Join(suite, "b.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "  b  ") || strings.Contains(stdout, "  a  ") {
		t.Fatalf("table:\n%s", stdout)
	}
	if want := []string{filepath.Join(suite, "skills", "x")}; !slices.Equal(factory.calls[0].skills, want) {
		t.Errorf("calls = %v", factory.calls)
	}
}

// TestRunPrintsTableAndWritesJSON checks what a finished run leaves behind.
// It asserts the table is printed, the JSON file is named after the clock, and its path is printed.
func TestRunPrintsTableAndWritesJSON(t *testing.T) {
	var out bytes.Buffer
	resultsDir := filepath.Join(t.TempDir(), "results")
	app := app{
		stdout:     &out,
		stderr:     &bytes.Buffer{},
		build:      (&fakeFactory{}).build,
		resultsDir: resultsDir,
		now:        func() time.Time { return time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC) },
	}
	if err := app.run(context.Background(), []string{"-agent", "ollama", "-models", "m", writeSuite(t)}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "TARGET") || !strings.Contains(out.String(), "ollama@m") {
		t.Errorf("table:\n%s", out.String())
	}
	path := filepath.Join(resultsDir, "20261103-100000.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"target": "ollama@m"`) {
		t.Errorf("json = %s", b)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("path not printed:\n%s", out.String())
	}
}

// TestRunAcceptsRetriesFlag checks -retries parses.
// It asserts a value is accepted and a non-number is refused.
func TestRunAcceptsRetriesFlag(t *testing.T) {
	suite := writeSuite(t)
	if _, _, err := execute(t, &fakeFactory{}, "-models", "m", "-retries", "3", suite); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, &fakeFactory{}, "-models", "m", "-retries", "lots", suite); err == nil {
		t.Fatal("want an error")
	}
}

// TestRunTreatsFolderAndFileOfOneSuiteAsOne checks suite folders are compared after cleaning.
// It asserts a folder with a trailing slash plus a case file in it still allows -skills, and
// that the file already covered by the folder runs only once.
func TestRunTreatsFolderAndFileOfOneSuiteAsOne(t *testing.T) {
	factory := &fakeFactory{}
	suite := writeSuite(t)
	stdout, _, err := execute(t, factory, "-agent", "pi", "-models", "p/m", "-skills", "x", suite+"/", filepath.Join(suite, "a.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(stdout, "  a  "); n != 1 {
		t.Errorf("case a appears %d times:\n%s", n, stdout)
	}
}

// TestRunRefusesSkillNamesThatLeaveTheSkillsFolder checks skill names are plain folder names.
// It asserts names with a slash or ".." fail before any target is built.
func TestRunRefusesSkillNamesThatLeaveTheSkillsFolder(t *testing.T) {
	for _, name := range []string{"../x", "x/y", "..", `a\b`} {
		factory := &fakeFactory{}
		_, _, err := execute(t, factory, "-agent", "pi", "-models", "p/m", "-skills", name, writeSuite(t))
		if err == nil || !strings.Contains(err.Error(), "plain folder name") {
			t.Errorf("%q: err = %v", name, err)
		}
		if len(factory.calls) != 0 {
			t.Errorf("%q: calls = %v", name, factory.calls)
		}
	}
}

// TestRealTarget checks the factory used by the command.
// It asserts pi gives a target.Pi named pi@m with the skill dirs, and ollama gives a target.Ollama.
func TestRealTarget(t *testing.T) {
	pi, ok := realTarget("pi", "m", "http://h", []string{"/s/skills/x"}).(target.Pi)
	if !ok || pi.Name() != "pi+x@m" || !slices.Equal(pi.Skills, []string{"/s/skills/x"}) {
		t.Errorf("pi = %+v", pi)
	}
	if noSkills := realTarget("pi", "m", "http://h", nil); noSkills.Name() != "pi@m" {
		t.Errorf("name = %q", noSkills.Name())
	}
	o, ok := realTarget("ollama", "m", "http://h", nil).(target.Ollama)
	if !ok || o.Model != "m" || o.Client.Host != "http://h" {
		t.Errorf("ollama = %+v", o)
	}
}

type retryingTarget struct {
	calls *int
}

func (r retryingTarget) Name() string { return "retrying" }

func (r retryingTarget) Run(ctx context.Context, in target.Input) (target.Output, error) {
	*r.calls++
	return target.Output{}, target.RetryError{Reason: "try again"}
}

// TestRunWiresRetriesIntoTheRunner checks the -retries flag reaches the runner.
// It asserts -retries 3 makes four tries of a target that always asks for a retry, and the
// default of 2 makes three.
func TestRunWiresRetriesIntoTheRunner(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"-retries", "3"}, 4},
		{nil, 3},
	} {
		calls := 0
		app := app{
			stdout:     &bytes.Buffer{},
			stderr:     &bytes.Buffer{},
			build:      func(agent, model, host string, skillDirs []string) target.Target { return retryingTarget{&calls} },
			resultsDir: filepath.Join(t.TempDir(), "results"),
			now:        time.Now,
		}
		args := append(tc.args, "-models", "m", filepath.Join(writeSuite(t), "a.yaml"))
		if err := app.run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		if calls != tc.want {
			t.Errorf("%v: calls = %d, want %d", tc.args, calls, tc.want)
		}
	}
}

// TestRunRejectsNegativeRetries checks -retries must not be below zero.
// It asserts -retries -1 fails with "-retries must be 0 or more".
func TestRunRejectsNegativeRetries(t *testing.T) {
	_, _, err := execute(t, &fakeFactory{}, "-models", "m", "-retries", "-1", writeSuite(t))
	if err == nil || !strings.Contains(err.Error(), "-retries must be 0 or more") {
		t.Fatalf("err = %v", err)
	}
}

// TestRealTargetPiUsesThePiBinary checks the real Pi target is set up to start pi.
// It asserts the Pi target's Bin is "pi".
func TestRealTargetPiUsesThePiBinary(t *testing.T) {
	pi := realTarget("pi", "ollama/m", "http://h", nil).(target.Pi)
	if pi.Bin != "pi" {
		t.Errorf("bin = %q", pi.Bin)
	}
}

// TestRunGivesPiAbsoluteSkillDirs checks skill folders don't depend on where pi runs.
// It asserts a relative suite path still gives skill dirs that are absolute.
func TestRunGivesPiAbsoluteSkillDirs(t *testing.T) {
	factory := &fakeFactory{}
	suite := writeSuite(t)
	wd, _ := os.Getwd()
	rel, err := filepath.Rel(wd, suite)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, factory, "-agent", "pi", "-models", "p/m", "-skills", "x", rel); err != nil {
		t.Fatal(err)
	}
	if got := factory.calls[0].skills[0]; !filepath.IsAbs(got) || got != filepath.Join(suite, "skills", "x") {
		t.Errorf("skill dir = %q", got)
	}
}

// TestRunChecksPiModelsHaveAProvider checks -models values for -agent pi.
// It asserts a value with no slash fails naming it, before any target is built, while ollama accepts it.
func TestRunChecksPiModelsHaveAProvider(t *testing.T) {
	factory := &fakeFactory{}
	_, _, err := execute(t, factory, "-agent", "pi", "-models", "ollama/ok,gpt-oss:20b", writeSuite(t))
	if err == nil || !strings.Contains(err.Error(), `"gpt-oss:20b"`) || !strings.Contains(err.Error(), "provider/model") {
		t.Fatalf("err = %v", err)
	}
	if len(factory.calls) != 0 {
		t.Errorf("calls = %v", factory.calls)
	}
	if _, _, err := execute(t, &fakeFactory{}, "-agent", "ollama", "-models", "gpt-oss:20b", writeSuite(t)); err != nil {
		t.Errorf("ollama: %v", err)
	}
}

// TestRunResolvesSymlinksInSkillDirs checks skill folders are the real paths.
// It asserts a suite reached through a symlink gives skill dirs with the symlink resolved,
// so they match the paths pi reports.
func TestRunResolvesSymlinksInSkillDirs(t *testing.T) {
	factory := &fakeFactory{}
	suite := writeSuite(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(suite, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, factory, "-agent", "pi", "-models", "p/m", "-skills", "x", link); err != nil {
		t.Fatal(err)
	}
	if got := factory.calls[0].skills[0]; got != filepath.Join(suite, "skills", "x") {
		t.Errorf("skill dir = %q", got)
	}
}

// TestRunPrintsGridFrame checks the grid under the table for two models and two skill sets.
// It asserts the header names both models in -models order, the rows are none then x,
// and every cell is "-" for now.
func TestRunPrintsGridFrame(t *testing.T) {
	stdout, _, err := execute(t, &fakeFactory{}, "-agent", "pi", "-models", "p/a,p/b", "-skills", "x", "-skills", "none", writeSuite(t))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "SKILLS") || strings.HasPrefix(line, "none") || strings.HasPrefix(line, "x ") {
			lines = append(lines, strings.Fields(line)...)
		}
	}
	want := []string{"SKILLS", `\`, "MODEL", "p/a", "p/b", "none", "-", "-", "x", "-", "-"}
	if !slices.Equal(lines, want) {
		t.Errorf("grid fields = %q, want %q\n%s", lines, want, stdout)
	}
}

// TestRunPrintsGridForOllama checks a run without skills.
// It asserts the grid has one none row.
func TestRunPrintsGridForOllama(t *testing.T) {
	stdout, _, err := execute(t, &fakeFactory{}, "-models", "m", writeSuite(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "SKILLS \\ MODEL") || strings.Count(stdout, "\nnone") != 1 {
		t.Errorf("stdout:\n%s", stdout)
	}
}

// TestRunAcceptsMinPassWithOrWithoutPercent checks both spellings of -min-pass.
// It asserts 90%, 90 and the edges 1 and 100 are accepted.
func TestRunAcceptsMinPassWithOrWithoutPercent(t *testing.T) {
	for _, value := range []string{"90%", "90", "100", "1"} {
		if _, _, err := execute(t, &fakeFactory{}, "-models", "m", "-min-pass", value, writeSuite(t)); err != nil {
			t.Errorf("-min-pass %s: %v", value, err)
		}
	}
}

// TestRunRefusesBadMinPass checks -min-pass must be a number from 1 to 100.
// It asserts 0, 150, abc, NaN, a lone percent sign and a negative number fail, and the flag name appears once in the message.
func TestRunRefusesBadMinPass(t *testing.T) {
	for _, value := range []string{"0", "150", "abc", "%", "-5", "NaN", "nan", "NaN%"} {
		_, stderr, err := execute(t, &fakeFactory{}, "-models", "m", "-min-pass", value, writeSuite(t))
		if err == nil || !strings.Contains(stderr, "invalid value") || !strings.Contains(stderr, "must be a number from 1 to 100") || strings.Contains(stderr, "-min-pass must be") {
			t.Errorf("-min-pass %s: err = %v, stderr = %q", value, err, stderr)
		}
	}
}

// TestRunWritesResultsAndGridInJSON checks the shape of the results file.
// It asserts the file is an object with the results and a grid holding rows and columns.
func TestRunWritesResultsAndGridInJSON(t *testing.T) {
	resultsDir := t.TempDir()
	if _, _, err := runWith(t, answerTarget("fake@m", "x"), resultsDir, "-models", "m", writeSuite(t)); err != nil {
		t.Fatal(err)
	}
	var file struct {
		Results []struct {
			Case string `json:"case"`
		} `json:"results"`
		Grid struct {
			Rows    []string `json:"rows"`
			Columns []string `json:"columns"`
		} `json:"grid"`
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(resultsDir, "20261103-100000.json"))), &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Results) != 2 || !slices.Equal(file.Grid.Rows, []string{"none"}) || !slices.Equal(file.Grid.Columns, []string{"m"}) {
		t.Errorf("file = %+v", file)
	}
}
