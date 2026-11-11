package grader

import (
	"math"
	"slices"
	"strings"
	"testing"

	"ai-eval/internal/target"
)

// TestReadNumbersReadsFormats checks one number written in each supported style.
// Each row asserts the value with its scale applied and whether it is plain or a percent.
func TestReadNumbersReadsFormats(t *testing.T) {
	tests := []struct {
		text string
		want float64
		kind NumberKind
	}{
		{"416,161", 416161, PlainNumber},
		{"416161", 416161, PlainNumber},
		{"$416.2 billion", 416.2e9, PlainNumber},
		{"$416.2B", 416.2e9, PlainNumber},
		{"416.2bn", 416.2e9, PlainNumber},
		{"391,035 million", 391035e6, PlainNumber},
		{"$1.5 Trillion", 1.5e12, PlainNumber},
		{"12 thousand", 12000, PlainNumber},
		{"3k", 3000, PlainNumber},
		{"7 mn", 7e6, PlainNumber},
		{"2.5M", 2.5e6, PlainNumber},
		{"31.97%", 31.97, PercentNumber},
		{"-3.5%", -3.5, PercentNumber},
		{"-12", -12, PlainNumber},
		{"31.97 %", 31.97, PercentNumber},
		{"31.97\t%", 31.97, PercentNumber},
		{"\u22123.5%", -3.5, PercentNumber},
		{"4 M&A deals", 4, PlainNumber},
		{"5 m", 5, PlainNumber},
		{"5 k", 5, PlainNumber},
		{"2025", 2025, PlainNumber},
		{"1,234.5", 1234.5, PlainNumber},
		{"revenue was $90,678 million.", 90678e6, PlainNumber},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			got := ReadNumbers(tc.text)
			if len(got) != 1 || math.Abs(got[0].Value-tc.want) > 1e-6*math.Abs(tc.want) || got[0].Kind != tc.kind {
				t.Fatalf("got %+v, want %v %v", got, tc.want, tc.kind)
			}
		})
	}
}

// TestReadNumbersKeepsWordsWhole checks scale words are only read when they stand alone.
// It asserts "5 more" and "10 months" stay plain 5 and 10, and "2025 by" stays 2025.
func TestReadNumbersKeepsWordsWhole(t *testing.T) {
	tests := []struct {
		text string
		want []float64
	}{
		{"5 more", []float64{5}},
		{"10 months", []float64{10}},
		{"2025 by 2026", []float64{2025, 2026}},
		{"3 bananas", []float64{3}},
		{"2 kilos", []float64{2}},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			var values []float64
			for _, n := range ReadNumbers(tc.text) {
				values = append(values, n.Value)
			}
			if !slices.Equal(values, tc.want) {
				t.Fatalf("got %v, want %v", values, tc.want)
			}
		})
	}
}

// TestReadNumbersSkipsIdentifiers checks digits inside words are not read as numbers.
// It asserts names like Q3, v1.2, report2025.pdf, 3rd and 10x give nothing, and that a range
// like 2024-2025 reads two positive years rather than 2024 and -2025.
func TestReadNumbersSkipsIdentifiers(t *testing.T) {
	for _, text := range []string{"Q3", "v1.2", "report2025.pdf", "3rd", "10x", "abc_12", "10X", "3RD", "416,161USD", "no numbers here", ""} {
		if got := ReadNumbers(text); len(got) != 0 {
			t.Errorf("%q: got %+v", text, got)
		}
	}
	got := ReadNumbers("2024-2025")
	if len(got) != 2 || got[0].Value != 2024 || got[1].Value != 2025 {
		t.Errorf("range: got %+v", got)
	}
	got = ReadNumbers("5%-6%")
	if len(got) != 2 || got[0].Value != 5 || got[1].Value != 6 {
		t.Errorf("percent range: got %+v", got)
	}
}

// TestReadNumbersFindsManyInOrder checks a sentence with several numbers.
// It asserts they come back in the order written, with kinds, and that years stay plain numbers.
func TestReadNumbersFindsManyInOrder(t *testing.T) {
	got := ReadNumbers("In 2025 revenue was $416.2 billion, up 6.4% from 2024.")
	want := []Number{{2025, PlainNumber}, {416.2e9, PlainNumber}, {6.4, PercentNumber}, {2024, PlainNumber}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
}

// TestReadNumbersIgnoresHugeNumbers checks absurd input cannot produce NaN or Inf.
// It asserts a 400-digit number (ParseFloat fails) and a 300-digit number scaled by trillion
// (the product overflows) are both skipped, leaving only the 5 after them.
func TestReadNumbersIgnoresHugeNumbers(t *testing.T) {
	tooLong := strings.Repeat("9", 400)
	overflows := "1" + strings.Repeat("0", 300)
	for _, text := range []string{tooLong + " trillion 5", overflows + " trillion 5"} {
		got := ReadNumbers(text)
		if !slices.Equal(got, []Number{{5, PlainNumber}}) {
			t.Errorf("got %+v", got)
		}
	}
}

// TestNumberCheckMatchesFigures checks the number grader against answers with figures in other styles.
// Each row asserts 416161 million at 1% matches "$416.2 billion" and "416,161 million" but not
// "$410 billion", a percent matches a close percent but not the same digits without a percent
// sign, and a figure matches anywhere in the answer.
func TestNumberCheckMatchesFigures(t *testing.T) {
	revenue := NumberCheck{Expected: []ExpectedNumber{{"416161 million", Number{416161e6, PlainNumber}}}, Within: 0.01}
	margin := NumberCheck{Expected: []ExpectedNumber{{"31.97%", Number{31.97, PercentNumber}}}, Within: 0.01}
	tests := []struct {
		name   string
		check  NumberCheck
		answer string
		pass   bool
	}{
		{"billions", revenue, "Revenue was $416.2 billion.", true},
		{"millions with commas", revenue, "Revenue: 416,161 million in 2025", true},
		{"too far", revenue, "Revenue was $410 billion.", false},
		{"no numbers", revenue, "I could not find it", false},
		{"exactly on the edge", NumberCheck{Expected: []ExpectedNumber{{"2", Number{2, PlainNumber}}}, Within: 0.5}, "3", true},
		{"just past the edge", NumberCheck{Expected: []ExpectedNumber{{"2", Number{2, PlainNumber}}}, Within: 0.5}, "3.01", false},
		{"percent close", margin, "Margin was 32.0%.", true},
		{"percent without sign", margin, "Margin was 31.97 of sales", false},
		{"plain number is not a percent", margin, "Margin 31.97", false},
		{"percent is not a plain number", NumberCheck{Expected: []ExpectedNumber{{"32", Number{32, PlainNumber}}}, Within: 0.01}, "32%", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.check.Grade(target.Output{Answer: tc.answer})
			if s.Grader != "number" || s.Pass != tc.pass || (s.Value == 1) != tc.pass {
				t.Fatalf("got %+v", s)
			}
		})
	}
}

// TestNumberCheckNeedsEveryValue checks all expected figures are required.
// It asserts the value is the share found, pass needs all of them, and the note names the
// missing ones as written with the tolerance as a percent.
func TestNumberCheckNeedsEveryValue(t *testing.T) {
	g := NumberCheck{
		Expected: []ExpectedNumber{
			{"416161 million", Number{416161e6, PlainNumber}},
			{"391035 million", Number{391035e6, PlainNumber}},
			{"383285 million", Number{383285e6, PlainNumber}},
		},
		Within: 0.01,
	}
	s := g.Grade(target.Output{Answer: "2025: $416.2 billion"})
	if s.Pass || s.Value != 1.0/3 || s.Note != "no number within 1% of: 391035 million, 383285 million" {
		t.Fatalf("got %+v", s)
	}

	g.Within = 0.005
	s = g.Grade(target.Output{Answer: "$416.2 billion, $391.0 billion, $383.3 billion"})
	if !s.Pass || s.Value != 1 || s.Note != "" {
		t.Fatalf("got %+v", s)
	}
	s = g.Grade(target.Output{Answer: "nothing"})
	if s.Note != "no number within 0.5% of: 416161 million, 391035 million, 383285 million" {
		t.Fatalf("note = %q", s.Note)
	}
}
