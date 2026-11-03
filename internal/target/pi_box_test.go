package target

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

type boxCalls struct {
	made    []string
	removed int
}

func fakeSandbox(c *boxCalls, box Box, err error) func(fixture string, skills []string) (Box, error) {
	return func(fixture string, skills []string) (Box, error) {
		c.made = append(c.made, fixture)
		if err != nil {
			return Box{}, err
		}
		box.Remove = func() error {
			c.removed++
			return nil
		}
		return box, nil
	}
}

// TestPiRunsInTheBox checks pi is pointed at the sandbox and not at the repo.
// It asserts the working folder is box.Work and --skill gets the box skills only.
func TestPiRunsInTheBox(t *testing.T) {
	var calls []processCall
	var made boxCalls
	box := Box{Work: "/box/work", Skills: []string{"/box/skills/a"}}
	p := Pi{
		Model:   "x/m",
		Skills:  []string{"/repo/skills/a"},
		Process: fakeProcess(&calls, []byte(readsStream("/box/skills/a/SKILL.md")), nil),
		Sandbox: fakeSandbox(&made, box, nil),
	}

	if _, err := p.Run(context.Background(), Input{Prompt: "q", Dir: "/repo/fixture"}); err != nil {
		t.Fatal(err)
	}

	args := calls[0].args
	if calls[0].dir != "/box/work" || !slices.Contains(args, "/box/skills/a") || slices.Contains(args, "/repo/skills/a") {
		t.Errorf("dir = %q, args = %v", calls[0].dir, args)
	}
	if !slices.Equal(made.made, []string{"/repo/fixture"}) || made.removed != 1 {
		t.Errorf("made = %v, removed = %d", made.made, made.removed)
	}
}

// TestPiChecksSkillsAgainstTheBox checks the skill check uses the box paths.
// It asserts a read of the box SKILL.md, absolute or relative to box.Work, counts as loaded.
func TestPiChecksSkillsAgainstTheBox(t *testing.T) {
	for _, path := range []string{"/box/skills/a/SKILL.md", "../skills/a/SKILL.md"} {
		var calls []processCall
		var made boxCalls
		p := Pi{
			Model:   "x/m",
			Skills:  []string{"/repo/skills/a"},
			Process: fakeProcess(&calls, []byte(readsStream(path)), nil),
			Sandbox: fakeSandbox(&made, Box{Work: "/box/work", Skills: []string{"/box/skills/a"}}, nil),
		}

		out, err := p.Run(context.Background(), Input{Prompt: "q", Dir: "/repo/fixture"})

		if err != nil || !slices.Equal(out.Trace.SkillsLoaded, []string{"a"}) {
			t.Errorf("path %q: err = %v, loaded = %v", path, err, out.Trace.SkillsLoaded)
		}
	}
}

// TestPiDoesNotCountAReadOfTheRepoSkill checks a read outside the box is not a load.
// It asserts reading the repo skill path gives a retry error and nothing counts as loaded.
func TestPiDoesNotCountAReadOfTheRepoSkill(t *testing.T) {
	var calls []processCall
	var made boxCalls
	p := Pi{
		Model:   "x/m",
		Skills:  []string{"/repo/skills/a"},
		Process: fakeProcess(&calls, []byte(readsStream("/repo/skills/a/SKILL.md")), nil),
		Sandbox: fakeSandbox(&made, Box{Work: "/box/work", Skills: []string{"/box/skills/a"}}, nil),
	}

	out, err := p.Run(context.Background(), Input{Prompt: "q", Dir: "/repo/fixture"})

	var retry RetryError
	if !errors.As(err, &retry) || len(out.Trace.SkillsLoaded) != 0 {
		t.Fatalf("err = %v, loaded = %v", err, out.Trace.SkillsLoaded)
	}
}

// TestPiRemovesTheBoxWhateverHappens checks cleanup on every way a run can end.
// It asserts Remove is called once after success, a process error, a stream error and a timeout.
func TestPiRemovesTheBoxWhateverHappens(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name    string
		ctx     context.Context
		stdout  string
		procErr error
		wantErr bool
	}{
		{"success", context.Background(), readsStream(), nil, false},
		{"process error", context.Background(), "", errors.New("exit status 2"), true},
		{"stream error", context.Background(), "not json at all\n", nil, true},
		{"context ended", cancelled, "", context.DeadlineExceeded, true},
	}
	for _, tc := range tests {
		var calls []processCall
		var made boxCalls
		p := Pi{
			Model:   "x/m",
			Process: fakeProcess(&calls, []byte(tc.stdout), tc.procErr),
			Sandbox: fakeSandbox(&made, Box{Work: "/box/work"}, nil),
		}

		_, err := p.Run(tc.ctx, Input{Prompt: "q", Dir: "/repo/fixture"})

		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, want error: %v", tc.name, err, tc.wantErr)
		}
		if made.removed != 1 {
			t.Errorf("%s: removed = %d, want 1", tc.name, made.removed)
		}
	}
}

// TestPiStopsWhenTheSandboxFails checks a failed sandbox.
// It asserts pi never starts and the error names the fixture and the cause.
func TestPiStopsWhenTheSandboxFails(t *testing.T) {
	var calls []processCall
	var made boxCalls
	p := Pi{
		Model:   "x/m",
		Process: fakeProcess(&calls, nil, nil),
		Sandbox: fakeSandbox(&made, Box{}, errors.New("disk full")),
	}

	_, err := p.Run(context.Background(), Input{Prompt: "q", Dir: "/repo/fixture"})

	if err == nil || !strings.Contains(err.Error(), "/repo/fixture") || !strings.Contains(err.Error(), "disk full") || len(calls) != 0 {
		t.Fatalf("err = %v, calls = %d", err, len(calls))
	}
}

func boxWith(remove func() error) func(string, []string) (Box, error) {
	return func(string, []string) (Box, error) {
		return Box{Work: "/box/work", Remove: remove}, nil
	}
}

// TestPiAllowsABoxWithoutRemove checks a box that has nothing to remove.
// It asserts a nil Remove does not panic and the run succeeds.
func TestPiAllowsABoxWithoutRemove(t *testing.T) {
	var calls []processCall
	p := Pi{Model: "x/m", Process: fakeProcess(&calls, []byte(readsStream()), nil), Sandbox: boxWith(nil)}

	if _, err := p.Run(context.Background(), Input{Prompt: "q"}); err != nil {
		t.Fatal(err)
	}
}

// TestPiReportsALeftoverBox checks a failed Remove after a good run.
// It asserts the error reads remove sandbox: and carries the cause.
func TestPiReportsALeftoverBox(t *testing.T) {
	var calls []processCall
	p := Pi{
		Model:   "x/m",
		Process: fakeProcess(&calls, []byte(readsStream()), nil),
		Sandbox: boxWith(func() error { return errors.New("busy") }),
	}

	_, err := p.Run(context.Background(), Input{Prompt: "q"})

	if err == nil || err.Error() != "remove sandbox: busy" {
		t.Fatalf("err = %v", err)
	}
}

// TestPiKeepsTheRunErrorOverARemoveError checks a failed run whose Remove also fails.
// It asserts the run error is returned and the remove error is dropped.
func TestPiKeepsTheRunErrorOverARemoveError(t *testing.T) {
	var calls []processCall
	p := Pi{
		Model:   "x/m",
		Process: fakeProcess(&calls, nil, errors.New("exit status 2")),
		Sandbox: boxWith(func() error { return errors.New("busy") }),
	}

	_, err := p.Run(context.Background(), Input{Prompt: "q"})

	if err == nil || !strings.Contains(err.Error(), "exit status 2") || strings.Contains(err.Error(), "busy") {
		t.Fatalf("err = %v", err)
	}
}

func passThrough(fixture string, skills []string) (Box, error) {
	return Box{Work: fixture, Skills: skills}, nil
}

func boxWithFiles(order *[]string, files map[string][]byte, filesErr error) func(string, []string) (Box, error) {
	return func(string, []string) (Box, error) {
		return Box{
			Work: "/box/work",
			Remove: func() error {
				*order = append(*order, "remove")
				return nil
			},
			NewFiles: func() (map[string][]byte, error) {
				*order = append(*order, "files")
				return files, filesErr
			},
		}, nil
	}
}

// TestPiReturnsNewFilesOnEveryPath checks the files are read however the run ends.
// It asserts Output.Files holds the box files after success, process error, timeout error from process, stream error and a skill retry error.
func TestPiReturnsNewFilesOnEveryPath(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name    string
		ctx     context.Context
		stdout  string
		procErr error
		skills  []string
	}{
		{"success", context.Background(), readsStream(), nil, nil},
		{"process error", context.Background(), "", errors.New("exit status 2"), nil},
		{"timeout error from process", cancelled, "", context.DeadlineExceeded, nil},
		{"stream error", context.Background(), "not json at all\n", nil, nil},
		{"skill retry error", context.Background(), readsStream(), nil, []string{"/box/skills/a"}},
	}
	for _, tc := range tests {
		var calls []processCall
		var order []string
		p := Pi{
			Model:   "x/m",
			Skills:  tc.skills,
			Process: fakeProcess(&calls, []byte(tc.stdout), tc.procErr),
			Sandbox: boxWithFiles(&order, map[string][]byte{"report.md": []byte("hi")}, nil),
		}

		out, _ := p.Run(tc.ctx, Input{Prompt: "q"})

		if string(out.Files["report.md"]) != "hi" {
			t.Errorf("%s: files = %v", tc.name, out.Files)
		}
		if !slices.Equal(order, []string{"files", "remove"}) {
			t.Errorf("%s: order = %v", tc.name, order)
		}
	}
}

// TestPiKeepsRunErrorWhenNewFilesFails checks a failed file read.
// It asserts the error lands in Trace.FilesErr, the run's own error is unchanged, and no files are returned.
func TestPiKeepsRunErrorWhenNewFilesFails(t *testing.T) {
	var calls []processCall
	var order []string
	p := Pi{
		Model:   "x/m",
		Process: fakeProcess(&calls, nil, errors.New("exit status 2")),
		Sandbox: boxWithFiles(&order, nil, errors.New("walk failed")),
	}

	out, err := p.Run(context.Background(), Input{Prompt: "q"})

	if err == nil || err.Error() != "exit status 2" {
		t.Errorf("err = %v", err)
	}
	if out.Trace.FilesErr != "walk failed" || out.Files != nil {
		t.Errorf("FilesErr = %q, files = %v", out.Trace.FilesErr, out.Files)
	}
	if !slices.Equal(order, []string{"files", "remove"}) {
		t.Errorf("order = %v", order)
	}
}

// TestPiAllowsABoxWithoutNewFiles checks a box that has no way to list files.
// It asserts a nil NewFiles gives no files and no error.
func TestPiAllowsABoxWithoutNewFiles(t *testing.T) {
	var calls []processCall
	p := Pi{Model: "x/m", Process: fakeProcess(&calls, []byte(readsStream()), nil), Sandbox: boxWith(nil)}

	out, err := p.Run(context.Background(), Input{Prompt: "q"})

	if err != nil || out.Files != nil || out.Trace.FilesErr != "" {
		t.Fatalf("err = %v, files = %v, FilesErr = %q", err, out.Files, out.Trace.FilesErr)
	}
}
