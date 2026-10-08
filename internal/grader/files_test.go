package grader

import (
	"slices"
	"testing"

	"ai-eval/internal/target"
)

const sample = "../../testdata/sample"

// TestFilesScoresAnswers checks how an answer is scored against the files really in the fixture.
// Each row asserts recall (share of top-level entries named), the count of made-up filenames,
// and pass: all named and none made up. Naming a nested file like src/app.go is not made up,
// and prose like "e.g." is not mistaken for a filename.
func TestFilesScoresAnswers(t *testing.T) {
	g := Files{
		Expected: []string{"README.md", "main.go", "src"},
		Known:    []string{"README.md", "main.go", "src", "app.go"},
	}

	tests := []struct {
		name         string
		answer       string
		recall       float64
		hallucinated float64
		pass         bool
	}{
		{"all present", "Files: README.md, main.go and the src/ folder.", 1, 0, true},
		{"partial", "Only README.md is here.", 1.0 / 3, 0, false},
		{"hallucinated name", "README.md, main.go, src, package.json", 1, 1, false},
		{"nested known file is fine", "README.md, main.go, src/app.go", 1, 0, true},
		{"empty answer", "", 0, 0, false},
		{"prose with e.g. is not a file", "e.g. README.md, main.go, src", 1, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := g.Grade(target.Output{Answer: tc.answer})
			if s.Grader != "files" || s.Value != tc.recall || s.Metrics["recall"] != tc.recall ||
				s.Metrics["hallucinated"] != tc.hallucinated || s.Pass != tc.pass {
				t.Fatalf("got %+v, want recall=%v halluc=%v pass=%v", s, tc.recall, tc.hallucinated, tc.pass)
			}
		})
	}
}

// TestFilesNotesMadeUpNames checks a failing score explains itself.
// It asserts the note lists the made-up filenames so you don't have to dig through the answer.
func TestFilesNotesMadeUpNames(t *testing.T) {
	g := Files{Expected: []string{"README.md"}, Known: []string{"README.md"}}
	s := g.Grade(target.Output{Answer: "README.md, package.json, go.sum"})
	if s.Note != "made up: package.json, go.sum" {
		t.Errorf("note = %q", s.Note)
	}
}

// TestNewFilesReadsTheFixture checks the expected answer comes from disk, not from code.
// It asserts Expected holds only top-level entries (hidden files and folders included) and
// Known also holds the nested src/app.go. A missing folder gives an error.
func TestNewFilesReadsTheFixture(t *testing.T) {
	g, err := NewFiles(sample)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(g.Expected, ".env.example") || !slices.Contains(g.Expected, "src") || slices.Contains(g.Expected, "app.go") {
		t.Errorf("expected = %v", g.Expected)
	}
	if !slices.Contains(g.Known, "app.go") {
		t.Errorf("known = %v", g.Known)
	}

	if _, err := NewFiles("does/not/exist"); err == nil {
		t.Error("missing fixture should fail")
	}
}
