package grader

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"ai-eval/internal/target"
)

type NumberKind int

const (
	PlainNumber NumberKind = iota
	PercentNumber
)

type Number struct {
	Value float64
	Kind  NumberKind
}

var numberLike = regexp.MustCompile(`(-|\x{2212})?\$?(\d{1,3}(?:,\d{3})+|\d+)(\.\d+)?(?:([ \t]*%)|([ \t]?[A-Za-z]+))?`)

var scales = map[string]float64{
	"thousand": 1e3, "k": 1e3,
	"million": 1e6, "m": 1e6, "mn": 1e6,
	"billion": 1e9, "b": 1e9, "bn": 1e9,
	"trillion": 1e12,
}

func ReadNumbers(text string) []Number {
	var found []Number
	for _, m := range numberLike.FindAllStringSubmatchIndex(text, -1) {
		matchStart, minusEnd := m[0], m[3]
		wholeStart, wholeEnd := m[4], m[5]
		decimalStart, decimalEnd := m[6], m[7]
		percentStart, wordStart, wordEnd := m[8], m[10], m[11]

		afterMinus := matchStart
		if minusEnd >= 0 {
			afterMinus = minusEnd
		}
		if afterMinus > 0 && isDigit(text[afterMinus]) && touchesWord(text, afterMinus) {
			continue
		}

		digits := strings.ReplaceAll(text[wholeStart:wholeEnd], ",", "")
		if decimalStart >= 0 {
			digits += text[decimalStart:decimalEnd]
		}
		value, err := strconv.ParseFloat(digits, 64)
		if err != nil || math.IsInf(value, 0) {
			continue
		}
		if minusEnd >= 0 && !(matchStart > 0 && (isWordByte(text[matchStart-1]) || text[matchStart-1] == '%')) {
			value = -value
		}

		n := Number{Value: value}
		if percentStart >= 0 {
			n.Kind = PercentNumber
		} else if wordStart >= 0 {
			suffix := text[wordStart:wordEnd]
			word := strings.TrimLeft(suffix, " \t")
			glued := suffix == word
			scale, isScale := scales[strings.ToLower(word)]
			switch {
			case isScale && (glued || len(word) > 1):
				n.Value *= scale
			case glued && !isScale:
				continue
			}
		}
		if math.IsInf(n.Value, 0) {
			continue
		}
		found = append(found, n)
	}
	return found
}

func touchesWord(text string, i int) bool {
	prev := text[i-1]
	return isWordByte(prev) || prev == '.' && i > 1 && isDigit(text[i-2])
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isWordByte(c byte) bool {
	return c == '_' || isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

type ExpectedNumber struct {
	Text   string
	Number Number
}

type NumberCheck struct {
	Expected []ExpectedNumber
	Within   float64
}

func (NumberCheck) Name() string {
	return "number"
}

func (g NumberCheck) Grade(out target.Output) Score {
	s := Score{Grader: g.Name()}
	answer := ReadNumbers(out.Answer)
	var missing []string
	for _, want := range g.Expected {
		if !slices.ContainsFunc(answer, func(got Number) bool { return g.near(want.Number, got) }) {
			missing = append(missing, want.Text)
		}
	}
	s.Value = float64(len(g.Expected)-len(missing)) / float64(len(g.Expected))
	s.Pass = len(missing) == 0
	if len(missing) > 0 {
		percent := strconv.FormatFloat(g.Within*100, 'g', 6, 64)
		s.Note = "no number within " + percent + "% of: " + strings.Join(missing, ", ")
	}
	return s
}

func (g NumberCheck) near(want, got Number) bool {
	return got.Kind == want.Kind && math.Abs(got.Value-want.Value) <= g.Within*math.Abs(want.Value)
}
