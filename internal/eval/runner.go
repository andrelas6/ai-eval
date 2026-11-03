package eval

import (
	"context"
	"errors"
	"time"

	"ai-eval/internal/grader"
	"ai-eval/internal/target"
)

type Result struct {
	Target string         `json:"target"`
	Case   string         `json:"case"`
	N      int            `json:"n"`
	Output target.Output  `json:"output"`
	Err    string         `json:"error,omitempty"`
	Tries  int            `json:"tries"`
	Pass   bool           `json:"pass"`
	Scores []grader.Score `json:"scores,omitempty"`
}

type Runner struct {
	Timeout  time.Duration
	Retries  int
	OnResult func(Result)
	Logf     func(format string, args ...any)
}

func (r Runner) Run(ctx context.Context, targets []target.Target, cases []Case) []Result {
	var results []Result
	for _, t := range targets {
		if ctx.Err() != nil {
			return results
		}
		r.warmUp(ctx, t)
		for _, c := range cases {
			for n := 1; n <= c.Runs; n++ {
				if ctx.Err() != nil {
					return results
				}
				res := r.runOnce(ctx, t, c)
				res.N = n
				results = append(results, res)
				if r.OnResult != nil {
					r.OnResult(res)
				}
			}
		}
	}
	return results
}

type warmer interface {
	WarmUp(ctx context.Context) error
}

func (r Runner) warmUp(ctx context.Context, t target.Target) {
	w, ok := t.(warmer)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*r.Timeout)
	defer cancel()
	if err := w.WarmUp(ctx); err != nil && r.Logf != nil {
		r.Logf("%s: warm-up failed: %v", t.Name(), err)
	}
}

func (r Runner) runOnce(ctx context.Context, t target.Target, c Case) Result {
	var res Result
	var cost float64
	var tokens int
	for try := 1; ; try++ {
		var retry bool
		res, retry = r.tryOnce(ctx, t, c)
		res.Tries = try
		cost += res.Output.Trace.Cost
		tokens += res.Output.Trace.Tokens
		res.Output.Trace.Cost = cost
		res.Output.Trace.Tokens = tokens
		if !retry || try > r.Retries || ctx.Err() != nil {
			break
		}
		if r.Logf != nil {
			r.Logf("%s %s: try %d failed, retrying: %s", res.Target, res.Case, try, res.Err)
		}
	}
	return res
}

func (r Runner) tryOnce(ctx context.Context, t target.Target, c Case) (Result, bool) {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	out, err := t.Run(ctx, target.Input{System: c.System, Prompt: c.Prompt, Tools: c.Tools, Dir: c.Fixture})
	res := Result{Target: t.Name(), Case: c.ID, Output: out}
	if err != nil {
		res.Err = err.Error()
		var retry target.RetryError
		return res, errors.As(err, &retry)
	}

	res.Pass = true
	for _, g := range c.Graders {
		s := g.Grade(out)
		res.Scores = append(res.Scores, s)
		res.Pass = res.Pass && s.Pass
	}
	return res, false
}
