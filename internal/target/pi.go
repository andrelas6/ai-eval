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
	Sandbox  func(fixture string, skills []string) (Box, error)
}

type Box struct {
	Root     string
	Work     string
	Skills   []string
	Remove   func() error
	NewFiles func() (map[string][]byte, error)
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

func (p Pi) Run(ctx context.Context, in Input) (out Output, err error) {
	provider, model, ok := strings.Cut(p.Model, "/")
	if !ok || provider == "" || model == "" {
		return Output{}, fmt.Errorf("model %q must be written provider/model, e.g. ollama/gpt-oss:20b", p.Model)
	}

	sandbox := p.Sandbox
	if sandbox == nil {
		sandbox = makeSandbox
	}
	box, err := sandbox(in.Dir, p.Skills)
	if err != nil {
		return Output{}, fmt.Errorf("sandbox for fixture %q: %w", in.Dir, err)
	}
	defer func() {
		if box.NewFiles != nil {
			files, filesErr := box.NewFiles()
			out.Files = files
			if filesErr != nil {
				out.Trace.FilesErr = filesErr.Error()
			}
		}
		if box.Remove != nil {
			if removeErr := box.Remove(); removeErr != nil && err == nil {
				err = fmt.Errorf("remove sandbox: %w", removeErr)
			}
		}
	}()

	process := p.Process
	agent := ""
	if process == nil {
		process = execProcess
		agent = piVersion(ctx, p.Bin)
	}

	start := time.Now()
	stdout, stderr, err := process(ctx, p.Bin, p.args(provider, model, box.Skills, in), box.Work)
	if err != nil {
		return Output{Trace: Trace{Wall: time.Since(start), Agent: agent, Sandbox: box.Root}}, err
	}
	out, err = parsePiEvents(stdout)
	out.Trace.Wall = time.Since(start)
	out.Trace.Agent = agent
	out.Trace.Sandbox = box.Root
	if err != nil {
		return out, withStderrTail(err, stderr)
	}
	out.Trace.SkillsOffered, out.Trace.SkillsLoaded, err = checkSkillsLoaded(out.Trace.ToolCalls(), box.Skills, box.Work)
	return out, err
}

func (p Pi) args(provider, model string, skills []string, in Input) []string {
	thinking := p.Thinking
	if thinking == "" {
		thinking = "medium"
	}
	args := []string{
		"-p", "--mode", "json", "--no-session", "--no-skills", "--no-context-files", "--no-extensions",
		"--no-prompt-templates", "--no-approve", "--tools", "read,bash", "--thinking", thinking,
		"--provider", provider, "--model", model,
	}
	for _, dir := range skills {
		args = append(args, "--skill", dir)
	}
	if in.System != "" {
		args = append(args, "--append-system-prompt", in.System)
	}
	return append(args, in.Prompt)
}
