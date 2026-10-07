package tool

import (
	"context"
	"fmt"
)

type Spec struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type Tool interface {
	Spec() Spec
	Call(ctx context.Context, args map[string]any) (string, error)
}

var registry = map[string]func(fixture string) Tool{
	"list_dir": func(fixture string) Tool { return ListDir{Root: fixture} },
}

func Build(names []string, fixture string) ([]Tool, error) {
	tools := make([]Tool, 0, len(names))
	for _, name := range names {
		newTool, ok := registry[name]
		if !ok {
			return nil, fmt.Errorf("unknown tool %q", name)
		}
		tools = append(tools, newTool(fixture))
	}
	return tools, nil
}
