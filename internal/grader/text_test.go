package grader

import (
	"regexp"
	"testing"

	"ai-eval/internal/target"
)

// TestContainsScoresAnswers checks the contains grader against several answers.
// Each row asserts the value is the share of items found, pass needs every item, case is
// ignored, and the note names the missing items.
func TestContainsScoresAnswers(t *testing.T) {
	g := Contains{Items: []string{"2025", "Revenue"}}
	tests := []struct {
		name   string
		answer string
		value  float64
		pass   bool
		note   string
	}{
		{"all present", "REVENUE in 2025", 1, true, ""},
		{"one missing", "revenue grew", 0.5, false, "missing: 2025"},
		{"none present", "nothing", 0, false, "missing: 2025, Revenue"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := g.Grade(target.Output{Answer: tc.answer})
			if s.Grader != "contains" || s.Value != tc.value || s.Pass != tc.pass || s.Note != tc.note {
				t.Fatalf("got %+v", s)
			}
		})
	}
}

// TestNotContainsScoresAnswers checks the not_contains grader against several answers.
// Each row asserts the value is the share of items absent, pass needs every item absent, case
// is ignored, and the note names the items found.
func TestNotContainsScoresAnswers(t *testing.T) {
	g := Contains{Items: []string{"unable to read", "cannot access"}, Absent: true}
	tests := []struct {
		name   string
		answer string
		value  float64
		pass   bool
		note   string
	}{
		{"clean", "Revenue was 416 billion", 1, true, ""},
		{"one found", "I am UNABLE TO READ the file", 0.5, false, "found: unable to read"},
		{"both found", "unable to read, cannot access", 0, false, "found: unable to read, cannot access"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := g.Grade(target.Output{Answer: tc.answer})
			if s.Grader != "not_contains" || s.Value != tc.value || s.Pass != tc.pass || s.Note != tc.note {
				t.Fatalf("got %+v", s)
			}
		})
	}
}

// TestRegexScoresAnswers checks the regex grader.
// It asserts a match anywhere passes with value 1, no match fails with the pattern in the note,
// matching is case-sensitive, and (?i) turns that off.
func TestRegexScoresAnswers(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		answer  string
		pass    bool
	}{
		{"matches anywhere", `\|\s*2025\s*\|`, "a | 2025 | b", true},
		{"no match", `\|\s*2025\s*\|`, "2025", false},
		{"case sensitive", `Revenue`, "revenue", false},
		{"ignore case", `(?i)Revenue`, "revenue", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Regex{Pattern: regexp.MustCompile(tc.pattern)}
			s := g.Grade(target.Output{Answer: tc.answer})
			if s.Grader != "regex" || s.Pass != tc.pass {
				t.Fatalf("got %+v", s)
			}
			if tc.pass && (s.Value != 1 || s.Note != "") {
				t.Fatalf("got %+v", s)
			}
			if !tc.pass && (s.Value != 0 || s.Note != "no match for "+tc.pattern) {
				t.Fatalf("got %+v", s)
			}
		})
	}
}
