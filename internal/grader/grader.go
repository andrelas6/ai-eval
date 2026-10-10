package grader

import "ai-eval/internal/target"

type Score struct {
	Grader  string             `json:"grader"`
	Value   float64            `json:"value"`
	Pass    bool               `json:"pass"`
	Note    string             `json:"note,omitempty"`
	Metrics map[string]float64 `json:"metrics,omitempty"`
}

type Grader interface {
	Name() string
	Grade(out target.Output) Score
}
