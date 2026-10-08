package target

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ai-eval/internal/tool"
)

type fakeModel struct {
	name string
	got  Input
	out  Output
	err  error
}

func (f *fakeModel) Name() string { return f.name }

func (f *fakeModel) Run(_ context.Context, in Input) (Output, error) {
	f.got = in
	return f.out, f.err
}

// TestSkillAddsInstructionsToTheModel checks what a skill sends to the model it wraps.
// It asserts the skill's instructions come first in the system prompt, followed by the case's
// own system prompt, while the user prompt and tools pass through unchanged and the model's
// output comes back untouched.
func TestSkillAddsInstructionsToTheModel(t *testing.T) {
	model := &fakeModel{name: "m", out: Output{Answer: "done"}}
	s, err := NewSkill("ae-review", "Review like André.", model)
	if err != nil {
		t.Fatal(err)
	}

	in := Input{System: "you can list files", Prompt: "review this", Tools: []tool.Tool{tool.ListDir{}}}
	out, err := s.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}

	if want := "Review like André.\n\nyou can list files"; model.got.System != want {
		t.Errorf("system = %q, want %q", model.got.System, want)
	}
	if model.got.Prompt != "review this" || len(model.got.Tools) != 1 {
		t.Errorf("input sent = %+v", model.got)
	}
	if out.Answer != "done" {
		t.Errorf("answer = %q", out.Answer)
	}
}

// TestSkillWithoutCaseSystemPrompt checks a case with no system prompt of its own.
// It asserts the model gets just the skill's instructions, with no trailing blank lines.
func TestSkillWithoutCaseSystemPrompt(t *testing.T) {
	model := &fakeModel{name: "m"}
	s, _ := NewSkill("ae-review", "Review like André.", model)

	s.Run(context.Background(), Input{Prompt: "review this"})

	if model.got.System != "Review like André." {
		t.Errorf("system = %q", model.got.System)
	}
}

// TestSkillPassesModelErrorsThrough checks a failing model run.
// It asserts the skill returns the model's error and its partial trace as they are.
func TestSkillPassesModelErrorsThrough(t *testing.T) {
	boom := errors.New("boom")
	model := &fakeModel{name: "m", out: Output{Trace: Trace{Turns: make([]Turn, 2)}}, err: boom}
	s, _ := NewSkill("ae-review", "x", model)

	out, err := s.Run(context.Background(), Input{})
	if !errors.Is(err, boom) || len(out.Trace.Turns) != 2 {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
}

// TestSkillNeedsAModel checks the model a skill runs on has to be chosen explicitly.
// It asserts NewSkill refuses a nil model, and a zero Skill refuses to run instead of
// falling back to some default model, and still has a name instead of crashing.
func TestSkillNeedsAModel(t *testing.T) {
	if _, err := NewSkill("ae-review", "x", nil); err == nil || err.Error() != `skill "ae-review" needs a model` {
		t.Errorf("NewSkill err = %v", err)
	}
	if _, err := (Skill{}).Run(context.Background(), Input{}); err == nil {
		t.Error("zero Skill should not run")
	}
	if got := (Skill{}).Name(); got != "@(no model)" {
		t.Errorf("zero Skill name = %q", got)
	}
}

// TestSkillName checks a skill shows up in reports as skill@model, so the same skill on
// different models gets separate rows.
func TestSkillName(t *testing.T) {
	s, _ := NewSkill("ae-review", "x", &fakeModel{name: "qwen3-coder:30b"})
	if got := s.Name(); got != "ae-review@qwen3-coder:30b" {
		t.Errorf("name = %q", got)
	}
}

// TestLoadSkill checks a skill is read from skills/<name>/SKILL.md.
// It asserts the name comes from the folder, the whole file becomes the instructions,
// and a folder without SKILL.md gives an error.
func TestLoadSkill(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ae-review")
	os.Mkdir(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: ae-review\n---\nReview like André.\n"), 0o644)
	model := &fakeModel{name: "m"}

	s, err := LoadSkill(dir, model)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "ae-review@m" {
		t.Errorf("name = %q", s.Name())
	}
	s.Run(context.Background(), Input{})
	if model.got.System != "---\nname: ae-review\n---\nReview like André." {
		t.Errorf("instructions = %q", model.got.System)
	}

	if _, err := LoadSkill(t.TempDir(), model); err == nil {
		t.Error("folder without SKILL.md should fail")
	}
}
