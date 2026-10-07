package main

import (
	"regexp"
	"strings"
)

// Result is the correctness score of one answer.
type Result struct {
	Recall       float64 `json:"recall"`       // share of expected entries named in the answer
	Hallucinated int     `json:"hallucinated"` // filename-like tokens that don't exist in the fixture
	Pass         bool    `json:"pass"`
}

// fileLike matches tokens such as "main.go", ".env.example", "package.json".
var fileLike = regexp.MustCompile(`[\w.-]+\.[A-Za-z]\w*`)

// Score checks answer against the expected top-level entries.
// known holds every name in the fixture tree, so mentioning a nested file is not a hallucination.
func Score(answer string, expected, known []string) Result {
	var r Result
	if len(expected) == 0 {
		return r
	}

	found := 0
	for _, name := range expected {
		if strings.Contains(answer, name) {
			found++
		}
	}
	r.Recall = float64(found) / float64(len(expected))

	knownSet := map[string]bool{}
	for _, k := range known {
		knownSet[strings.TrimLeft(k, ".")] = true
	}
	for _, tok := range fileLike.FindAllString(answer, -1) {
		if len(tok) < 4 { // skips prose like "e.g"
			continue
		}
		if !knownSet[strings.TrimLeft(tok, ".")] {
			r.Hallucinated++
		}
	}

	r.Pass = r.Recall == 1 && r.Hallucinated == 0
	return r
}
