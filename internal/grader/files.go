package grader

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"ai-eval/internal/target"
)

var fileLike = regexp.MustCompile(`[\w.-]+\.[A-Za-z]\w*`)

type Files struct {
	Expected []string
	Known    []string
}

func NewFiles(fixture string) (Files, error) {
	var g Files
	entries, err := os.ReadDir(fixture)
	if err != nil {
		return g, err
	}
	for _, e := range entries {
		g.Expected = append(g.Expected, e.Name())
	}
	err = filepath.WalkDir(fixture, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != fixture {
			g.Known = append(g.Known, d.Name())
		}
		return nil
	})
	return g, err
}

func (Files) Name() string {
	return "files"
}

func (g Files) Grade(out target.Output) Score {
	s := Score{Grader: g.Name(), Metrics: map[string]float64{"recall": 0, "hallucinated": 0}}
	if len(g.Expected) == 0 {
		return s
	}

	found := 0
	for _, name := range g.Expected {
		if strings.Contains(out.Answer, name) {
			found++
		}
	}
	recall := float64(found) / float64(len(g.Expected))

	known := map[string]bool{}
	for _, k := range g.Known {
		known[strings.TrimLeft(k, ".")] = true
	}
	hallucinated := 0
	var madeUp []string
	for _, tok := range fileLike.FindAllString(out.Answer, -1) {
		if len(tok) < 4 || known[strings.TrimLeft(tok, ".")] {
			continue
		}
		hallucinated++
		if !slices.Contains(madeUp, tok) {
			madeUp = append(madeUp, tok)
		}
	}

	s.Value = recall
	s.Metrics["recall"] = recall
	s.Metrics["hallucinated"] = float64(hallucinated)
	s.Pass = recall == 1 && hallucinated == 0
	if len(madeUp) > 0 {
		s.Note = "made up: " + strings.Join(madeUp, ", ")
	}
	return s
}
