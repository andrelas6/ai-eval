package main

import "testing"

// TestScore checks how an answer is scored against the fixture.
// Each row asserts recall (share of real entries named), the count of made-up filenames,
// and pass: all named, none made up, nested files allowed, and prose like "e.g." ignored.
func TestScore(t *testing.T) {
	expected := []string{"README.md", "main.go", "src"}
	known := []string{"README.md", "main.go", "src", "app.go"}

	tests := []struct {
		name         string
		answer       string
		recall       float64
		hallucinated int
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
			got := Score(tc.answer, expected, known)
			if got.Recall != tc.recall || got.Hallucinated != tc.hallucinated || got.Pass != tc.pass {
				t.Fatalf("got %+v, want recall=%v halluc=%d pass=%v", got, tc.recall, tc.hallucinated, tc.pass)
			}
		})
	}
}
