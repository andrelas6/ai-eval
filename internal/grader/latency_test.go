package grader

import (
	"testing"
	"time"

	"ai-eval/internal/target"
)

func took(d time.Duration) target.Output {
	return target.Output{Trace: target.Trace{Wall: d}}
}

// TestLatencyAgainstALimit checks a run is compared with the max time allowed.
// It asserts a run under or exactly at the limit passes, a slower one fails, and the
// run time in seconds is always recorded so the report can show p50 and p95.
func TestLatencyAgainstALimit(t *testing.T) {
	g := Latency{Max: 10 * time.Second}

	tests := []struct {
		wall time.Duration
		pass bool
	}{
		{2 * time.Second, true},
		{10 * time.Second, true},
		{11 * time.Second, false},
	}
	for _, tc := range tests {
		s := g.Grade(took(tc.wall))
		if s.Grader != "latency" || s.Pass != tc.pass || s.Metrics["wall_s"] != tc.wall.Seconds() {
			t.Errorf("wall %v: score = %+v", tc.wall, s)
		}
	}
}

// TestLatencyWithoutALimit checks a latency grader with no max set.
// It asserts every run passes and the time is still recorded, so speed can be measured
// without being judged.
func TestLatencyWithoutALimit(t *testing.T) {
	s := Latency{}.Grade(took(time.Minute))
	if !s.Pass || s.Metrics["wall_s"] != 60 {
		t.Errorf("score = %+v", s)
	}
}
