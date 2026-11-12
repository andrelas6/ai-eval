package grader

import (
	"regexp"
	"strings"

	"ai-eval/internal/target"
)

type Contains struct {
	Items  []string
	Absent bool
}

func (g Contains) Name() string {
	if g.Absent {
		return "not_contains"
	}
	return "contains"
}

func (g Contains) Grade(out target.Output) Score {
	s := Score{Grader: g.Name()}
	answer := strings.ToLower(out.Answer)
	passed := 0
	var failed []string
	for _, item := range g.Items {
		if strings.Contains(answer, strings.ToLower(item)) != g.Absent {
			passed++
		} else {
			failed = append(failed, item)
		}
	}
	s.Value = float64(passed) / float64(len(g.Items))
	s.Pass = len(failed) == 0
	if len(failed) > 0 {
		label := "missing: "
		if g.Absent {
			label = "found: "
		}
		s.Note = label + strings.Join(failed, ", ")
	}
	return s
}

type Regex struct {
	Pattern *regexp.Regexp
}

func (Regex) Name() string {
	return "regex"
}

func (g Regex) Grade(out target.Output) Score {
	s := Score{Grader: g.Name()}
	if g.Pattern.MatchString(out.Answer) {
		s.Value = 1
		s.Pass = true
	} else {
		s.Note = "no match for " + g.Pattern.String()
	}
	return s
}
