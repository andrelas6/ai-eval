package eval

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"ai-eval/internal/grader"
	"ai-eval/internal/tool"
)

type Case struct {
	ID      string
	Dir     string
	System  string
	Prompt  string
	Fixture string
	Tools   []tool.Tool
	Runs    int
	Graders []grader.Grader
}

var knownFields = []string{"id", "system", "prompt", "fixture", "tools", "runs", "graders"}

var graderBuilders = map[string]func(c Case, arg any) (grader.Grader, error){
	"files":        buildFiles,
	"tool_called":  buildToolCalled,
	"latency":      buildLatency,
	"contains":     buildContains,
	"not_contains": buildNotContains,
	"regex":        buildRegex,
	"number":       buildNumber,
}

func LoadCase(path string) (Case, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Case{}, err
	}
	m, err := parseYAML(string(b))
	if err != nil {
		return Case{}, fmt.Errorf("%s: %w", path, err)
	}
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	c, err := decodeCase(m, id, filepath.Dir(path))
	if err != nil {
		return Case{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func decodeCase(m map[string]any, defaultID, root string) (Case, error) {
	for key := range m {
		if !slices.Contains(knownFields, key) {
			return Case{}, fmt.Errorf("unknown field %q (known: %s)", key, strings.Join(knownFields, ", "))
		}
	}

	c := Case{ID: defaultID, Dir: root, Runs: 1}
	var err error
	if id, err := text(m, "id"); err != nil {
		return c, err
	} else if id != "" {
		c.ID = id
	}
	if c.System, err = text(m, "system"); err != nil {
		return c, err
	}
	if c.Prompt, err = text(m, "prompt"); err != nil {
		return c, err
	}
	if c.Prompt == "" {
		return c, errors.New(`"prompt" is required`)
	}

	if runs, err := text(m, "runs"); err != nil {
		return c, err
	} else if runs != "" {
		n, err := strconv.Atoi(runs)
		if err != nil || n < 1 {
			return c, fmt.Errorf(`"runs" must be a whole number above 0, got %q`, runs)
		}
		c.Runs = n
	}

	fixture, err := text(m, "fixture")
	if err != nil {
		return c, err
	}
	if fixture != "" {
		c.Fixture = filepath.Join(root, fixture)
		if info, err := os.Stat(c.Fixture); err != nil || !info.IsDir() {
			return c, fmt.Errorf("fixture %q not found", c.Fixture)
		}
		found, err := hasFiles(c.Fixture)
		if err != nil {
			return c, fmt.Errorf("fixture %q: %v", c.Fixture, err)
		}
		if !found {
			return c, fmt.Errorf("fixture %q is empty", c.Fixture)
		}
	}

	toolNames, err := list(m, "tools")
	if err != nil {
		return c, err
	}
	if len(toolNames) > 0 && c.Fixture == "" {
		return c, errors.New(`tools need a "fixture" folder to work in`)
	}
	names := make([]string, len(toolNames))
	for i, v := range toolNames {
		name, ok := v.(string)
		if !ok {
			return c, errors.New(`"tools" must be a list of tool names`)
		}
		names[i] = name
	}
	if c.Tools, err = tool.Build(names, c.Fixture); err != nil {
		return c, err
	}

	graders, err := list(m, "graders")
	if err != nil {
		return c, err
	}
	if len(graders) == 0 {
		return c, errors.New(`"graders" needs at least one grader`)
	}
	for _, item := range graders {
		g, err := buildGrader(c, item)
		if err != nil {
			return c, err
		}
		if slices.ContainsFunc(c.Graders, func(other grader.Grader) bool { return other.Name() == g.Name() }) {
			return c, fmt.Errorf("grader %q is listed twice; list all the values in one %s", g.Name(), g.Name())
		}
		c.Graders = append(c.Graders, g)
	}
	return c, nil
}

func hasFiles(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(entries, func(e os.DirEntry) bool { return !strings.HasPrefix(e.Name(), ".") }), nil
}

func buildGrader(c Case, item any) (grader.Grader, error) {
	var name string
	var arg any
	switch v := item.(type) {
	case string:
		name = v
	case map[string]any:
		if len(v) != 1 {
			return nil, errors.New("each grader must be a name or name: settings")
		}
		name = slices.Collect(maps.Keys(v))[0]
		arg = v[name]
	default:
		return nil, errors.New("each grader must be a name or name: settings")
	}

	build, ok := graderBuilders[name]
	if !ok {
		known := make([]string, 0, len(graderBuilders))
		for k := range graderBuilders {
			known = append(known, k)
		}
		slices.Sort(known)
		return nil, fmt.Errorf("unknown grader %q (known: %s)", name, strings.Join(known, ", "))
	}
	g, err := build(c, arg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return g, nil
}

func buildFiles(c Case, _ any) (grader.Grader, error) {
	if c.Fixture == "" {
		return nil, errors.New(`needs a "fixture" to compare the answer with`)
	}
	return grader.NewFiles(c.Fixture)
}

func buildToolCalled(c Case, arg any) (grader.Grader, error) {
	name, _ := arg.(string)
	if name == "" {
		return nil, errors.New("say which tool, e.g. tool_called: list_dir")
	}
	for _, t := range c.Tools {
		if t.Spec().Name == name {
			return grader.ToolCalled{Tool: name}, nil
		}
	}
	return nil, fmt.Errorf("the case doesn't offer %q in tools", name)
}

func buildLatency(_ Case, arg any) (grader.Grader, error) {
	var max string
	switch v := arg.(type) {
	case nil:
		return grader.Latency{}, nil
	case string:
		max = v
	case map[string]any:
		rest := maps.Clone(v)
		delete(rest, "max")
		if len(rest) > 0 {
			return nil, unknownSetting(rest)
		}
		max, _ = v["max"].(string)
	}
	d, err := time.ParseDuration(max)
	if err != nil || d <= 0 {
		return nil, fmt.Errorf(`"max" must be a duration like 10s, got %q`, max)
	}
	return grader.Latency{Max: d}, nil
}

func buildContains(_ Case, arg any) (grader.Grader, error) {
	items, err := textItems(arg)
	if err != nil {
		return nil, err
	}
	return grader.Contains{Items: items}, nil
}

func buildNotContains(_ Case, arg any) (grader.Grader, error) {
	items, err := textItems(arg)
	if err != nil {
		return nil, err
	}
	return grader.Contains{Items: items, Absent: true}, nil
}

func buildRegex(_ Case, arg any) (grader.Grader, error) {
	if m, ok := arg.(map[string]any); ok {
		return nil, unknownSetting(m)
	}
	if _, ok := arg.([]any); ok {
		return nil, errors.New("takes one pattern, not a list")
	}
	pattern, _ := arg.(string)
	if pattern == "" {
		return nil, errors.New("give a pattern, e.g. regex: '(?i)total'")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("bad pattern: %v", err)
	}
	return grader.Regex{Pattern: re}, nil
}

func buildNumber(_ Case, arg any) (grader.Grader, error) {
	settings, ok := arg.(map[string]any)
	if !ok {
		return nil, errors.New("give settings, e.g. number: {values: [416161 million], within: 1%}")
	}
	rest := maps.Clone(settings)
	delete(rest, "values")
	delete(rest, "within")
	if len(rest) > 0 {
		return nil, unknownSetting(rest)
	}

	rawValues, ok := settings["values"]
	if !ok {
		return nil, errors.New(`"values" needs at least one number, e.g. values: [416161 million]`)
	}
	items, ok := rawValues.([]any)
	if !ok {
		return nil, errors.New(`"values" must be a list, e.g. values: [416161 million]`)
	}
	if len(items) == 0 {
		return nil, errors.New(`"values" needs at least one number, e.g. values: [416161 million]`)
	}
	check := grader.NumberCheck{Within: 0.01}
	for _, item := range items {
		written, ok := item.(string)
		if !ok {
			return nil, errors.New(`"values" must be a list of text`)
		}
		found := grader.ReadNumbers(written)
		if len(found) != 1 {
			return nil, fmt.Errorf("value %q must hold exactly one number", written)
		}
		check.Expected = append(check.Expected, grader.ExpectedNumber{Text: written, Number: found[0]})
	}

	if raw, ok := settings["within"]; ok {
		written, ok := raw.(string)
		if !ok {
			return nil, errors.New(`"within" must be text like 1%`)
		}
		percent, err := strconv.ParseFloat(strings.TrimSuffix(written, "%"), 64)
		if !strings.HasSuffix(written, "%") || err != nil || !(percent > 0) || math.IsInf(percent, 0) {
			return nil, fmt.Errorf(`"within" must be a percent above 0 like 1%%, got %q`, written)
		}
		check.Within = percent / 100
	}
	return check, nil
}

func textItems(arg any) ([]string, error) {
	var raw []any
	switch v := arg.(type) {
	case string:
		raw = []any{v}
	case []any:
		raw = v
	case map[string]any:
		return nil, unknownSetting(v)
	}
	var items []string
	for _, r := range raw {
		s, ok := r.(string)
		if !ok {
			return nil, errors.New("must be text or a list of text")
		}
		if strings.TrimSpace(s) == "" {
			return nil, errors.New(`give at least one text, e.g. ["revenue", "2025"]`)
		}
		items = append(items, s)
	}
	if len(items) == 0 {
		return nil, errors.New(`give at least one text, e.g. ["revenue", "2025"]`)
	}
	return items, nil
}

func unknownSetting(m map[string]any) error {
	keys := slices.Sorted(maps.Keys(m))
	if len(keys) == 0 {
		return errors.New("give a value, not an empty map")
	}
	return fmt.Errorf("unknown setting %q", keys[0])
}

func text(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%q must be text", key)
	}
	return s, nil
}

func list(m map[string]any, key string) ([]any, error) {
	v, ok := m[key]
	if !ok || v == "" {
		return nil, nil
	}
	l, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%q must be a list", key)
	}
	return l, nil
}
