package grader

import (
	"math"
	"slices"
	"strings"
	"testing"
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
