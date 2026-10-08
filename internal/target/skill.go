package target

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Skill struct {
	name         string
	instructions string
	model        Target
}

func NewSkill(name, instructions string, model Target) (Skill, error) {
	if model == nil {
		return Skill{}, fmt.Errorf("skill %q needs a model", name)
	}
	return Skill{name: name, instructions: strings.TrimSpace(instructions), model: model}, nil
}

func LoadSkill(dir string, model Target) (Skill, error) {
	b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Skill{}, err
	}
	return NewSkill(filepath.Base(dir), string(b), model)
}

func (s Skill) Name() string {
	if s.model == nil {
		return s.name + "@(no model)"
	}
	return s.name + "@" + s.model.Name()
}

func (s Skill) Run(ctx context.Context, in Input) (Output, error) {
	if s.model == nil {
		return Output{}, errors.New("skill has no model, create it with NewSkill or LoadSkill")
	}
	in.System = strings.TrimSpace(s.instructions + "\n\n" + in.System)
	return s.model.Run(ctx, in)
}
