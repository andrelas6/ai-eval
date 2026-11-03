package target

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type Pi struct {
	Bin      string
	Model    string
	Skills   []string
	Thinking string
	Process  func(ctx context.Context, bin string, args []string, dir string) (stdout, stderr []byte, err error)
}

func (p Pi) Name() string {
	if len(p.Skills) == 0 {
		return "pi@" + p.Model
	}
	names := make([]string, len(p.Skills))
	for i, dir := range p.Skills {
		names[i] = filepath.Base(dir)
	}
	return "pi+" + strings.Join(names, ",") + "@" + p.Model
}

func (p Pi) Run(ctx context.Context, in Input) (Output, error) {
	provider, model, ok := strings.Cut(p.Model, "/")
	if !ok || provider == "" || model == "" {
		return Output{}, fmt.Errorf("model %q must be written provider/model, e.g. ollama/gpt-oss:20b", p.Model)
	}

	process := p.Process
	agent := ""
	if process == nil {
		process = execProcess
		agent = piVersion(ctx, p.Bin)
	}

	start := time.Now()
	stdout, stderr, err := process(ctx, p.Bin, p.args(provider, model, in), in.Dir)
	if err != nil {
		return Output{Trace: Trace{Wall: time.Since(start), Agent: agent}}, err
	}
	out, err := parsePiEvents(stdout)
	out.Trace.Wall = time.Since(start)
	out.Trace.Agent = agent
	if err != nil {
		return out, withStderrTail(err, stderr)
	}
	out.Trace.SkillsOffered, out.Trace.SkillsLoaded, err = checkSkillsLoaded(out.Trace.ToolCalls(), p.Skills, in.Dir)
	return out, err
}

func (p Pi) args(provider, model string, in Input) []string {
	thinking := p.Thinking
	if thinking == "" {
		thinking = "medium"
	}
	args := []string{
		"-p", "--mode", "json", "--no-session", "--no-skills", "--no-context-files", "--no-extensions",
		"--no-prompt-templates", "--no-approve", "--tools", "read,bash", "--thinking", thinking,
		"--provider", provider, "--model", model,
	}
	for _, dir := range p.Skills {
		args = append(args, "--skill", dir)
	}
	if in.System != "" {
		args = append(args, "--append-system-prompt", in.System)
	}
	return append(args, in.Prompt)
}
