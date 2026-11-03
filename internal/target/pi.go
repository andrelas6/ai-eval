package target

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

type Pi struct {
	Model  string
	Skills []string
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
	return Output{}, errors.New("pi target not built yet")
}
