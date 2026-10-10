package eval

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseYAMLCaseFile checks the parser reads a full case file the way we plan to write them.
// It asserts nested maps, a block list, an inline list, list items that are plain words,
// "key: value" pairs or inline maps, and that every value comes back as a string.
func TestParseYAMLCaseFile(t *testing.T) {
	src := `# evals/list-files.yaml
id: list-files
prompt: list the files of this directory
fixture: fixtures/sample
tools: [list_dir]
runs: 5
graders:
  - files
  - tool_called: list_dir
  - latency: { max: 10s }
`
	want := map[string]any{
		"id":      "list-files",
		"prompt":  "list the files of this directory",
		"fixture": "fixtures/sample",
		"tools":   []any{"list_dir"},
		"runs":    "5",
		"graders": []any{
			"files",
			map[string]any{"tool_called": "list_dir"},
			map[string]any{"latency": map[string]any{"max": "10s"}},
		},
	}

	got, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

// TestParseYAMLNesting checks the indentation rules.
// Each row asserts a nested map, a list at the same indent as its key, a list item that is
// a map with several keys, and a list item whose content starts on the next line.
func TestParseYAMLNesting(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want map[string]any
	}{
		{
			"nested map",
			"target:\n  model: qwen3.5:9b\n  skill:\n    path: skills/ae-review\n",
			map[string]any{"target": map[string]any{
				"model": "qwen3.5:9b",
				"skill": map[string]any{"path": "skills/ae-review"},
			}},
		},
		{
			"list at the same indent as its key",
			"tools:\n- list_dir\n- read_file\nruns: 2\n",
			map[string]any{"tools": []any{"list_dir", "read_file"}, "runs": "2"},
		},
		{
			"list item with several keys",
			"graders:\n  - latency: 10s\n    note: slow box\n  - files\n",
			map[string]any{"graders": []any{
				map[string]any{"latency": "10s", "note": "slow box"},
				"files",
			}},
		},
		{
			"list item starting on the next line",
			"graders:\n  -\n    files: yes\n",
			map[string]any{"graders": []any{map[string]any{"files": "yes"}}},
		},
		{
			"empty value",
			"system:\nprompt: hi\n",
			map[string]any{"system": "", "prompt": "hi"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseYAML(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

// TestParseYAMLScalars checks how single values are read.
// Each row asserts comments are dropped, quotes keep "#", ":" and "," as text, colons inside
// a plain value (like a model tag) are kept, and inline lists and maps split on commas
// outside quotes.
func TestParseYAMLScalars(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want any
	}{
		{"comment after value", "v: hello # a comment", "hello"},
		{"hash without space is text", "v: issue#12", "issue#12"},
		{"double quotes keep hash", `v: "list # files"  # real comment`, "list # files"},
		{"single quotes", `v: 'a: b, c'`, "a: b, c"},
		{"escaped double quote", `v: "say \"hi\""`, `say "hi"`},
		{"colon in plain value", "v: qwen3-coder:30b", "qwen3-coder:30b"},
		{"inline list", `v: [a, "b, c", d]`, []any{"a", "b, c", "d"}},
		{"empty inline list", "v: []", []any{}},
		{"inline map", `v: { max: 10s, note: "x, y" }`, map[string]any{"max": "10s", "note": "x, y"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseYAML(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if v := got["v"]; !reflect.DeepEqual(v, tc.want) {
				t.Errorf("got %#v, want %#v", v, tc.want)
			}
		})
	}
}

// TestParseYAMLEmpty checks a file with only comments and blank lines gives an empty map.
func TestParseYAMLEmpty(t *testing.T) {
	got, err := parseYAML("# nothing yet\n\n")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %#v, err = %v", got, err)
	}
}

// TestParseYAMLErrors checks YAML we don't support is refused instead of being misread.
// Each row asserts the error names the line it happened on and says what went wrong.
func TestParseYAMLErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"tab indent", "a:\n\tb: c", "line 2: tabs are not allowed"},
		{"text block", "prompt: |\n  hi", "line 1: | and > text blocks are not supported"},
		{"folded block", "prompt: >\n  hi", "line 1: | and > text blocks are not supported"},
		{"anchor", "a: &x 1", "line 1: anchors, aliases and tags are not supported"},
		{"alias", "a: *x", "line 1: anchors, aliases and tags are not supported"},
		{"tag", "a: !!str 1", "line 1: anchors, aliases and tags are not supported"},
		{"duplicate key", "a: 1\nb: 2\na: 3", `line 3: duplicate key "a"`},
		{"unclosed quote", `a: "hi`, "line 1: unclosed quote"},
		{"unclosed inline list", "a: [x, y", "line 1: unclosed ["},
		{"unclosed inline map", "a: { x: 1", "line 1: unclosed {"},
		{"line without a key", "a: 1\njust words", `line 2: expected "key: value"`},
		{"unexpected indent", "a: 1\n    b: 2", "line 2: unexpected indent"},
		{"list mixed into a map", "a: 1\n- b", "line 2: expected \"key: value\""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseYAML(tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// FuzzParseYAML throws random text at the parser.
// It asserts the parser never crashes: any input either parses or returns an error.
func FuzzParseYAML(f *testing.F) {
	for _, seed := range []string{
		"id: x\ngraders:\n  - files\n  - latency: { max: 10s }\n",
		"tools:\n- a\n-\n  b: c\n",
		`v: [a, "b, c", {x: 'y'}]`,
		"a: \"unclosed\n\t- b",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		parseYAML(src)
	})
}
