package grader

import (
	"fmt"
	"time"

	"ai-eval/internal/target"
)

type Latency struct {
	Max time.Duration
}

func (Latency) Name() string {
	return "latency"
}

func (g Latency) Grade(out target.Output) Score {
	wall := out.Trace.Wall
	s := Score{Grader: g.Name(), Metrics: map[string]float64{"wall_s": wall.Seconds()}}
	s.Pass = g.Max == 0 || wall <= g.Max
	if s.Pass {
		s.Value = 1
	} else {
		s.Note = fmt.Sprintf("took %.1fs, max is %.1fs", wall.Seconds(), g.Max.Seconds())
	}
	return s
}
