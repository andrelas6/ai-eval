package grader

import (
	"testing"

	"ai-eval/internal/target"
)

func filesOut(files map[string]string) target.Output {
	out := target.Output{}
	if files != nil {
		out.Files = map[string][]byte{}
	}
	for name, body := range files {
		out.Files[name] = []byte(body)
	}
	return out
}

// TestFileExistsMatchesPatterns checks the file_exists grader against written files.
// Each row asserts an exact name passes, *.md matches top-level markdown files only and not
// sub/x.md, a miss lists the files that were written (sorted), and no files at all says
// the target wrote no files.
func TestFileExistsMatchesPatterns(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		files   map[string]string
		pass    bool
		note    string
	}{
		{"exact name", "report.md", map[string]string{"report.md": "x"}, true, ""},
		{"star matches markdown", "*.md", map[string]string{"notes.md": "x"}, true, ""},
		{"star skips folders", "*.md", map[string]string{"sub/x.md": "x", "b.txt": "x", "a.txt": "x"}, false, "no file matches *.md; wrote: a.txt, b.txt, sub/x.md"},
		{"no files", "report.md", nil, false, "the target wrote no files"},
		{"empty files", "report.md", map[string]string{}, false, "the target wrote no files"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := FileExists{Pattern: tc.pattern}.Grade(filesOut(tc.files))
			wantValue := 0.0
			if tc.pass {
				wantValue = 1
			}
			if s.Grader != "file_exists" || s.Pass != tc.pass || s.Value != wantValue || s.Note != tc.note {
				t.Fatalf("got %+v", s)
			}
		})
	}
}

// TestFileExistsCapsTheFileList checks a long list of written files stays short.
// It asserts the note names the first five sorted files and counts the rest.
func TestFileExistsCapsTheFileList(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		files[name+".txt"] = "x"
	}
	s := FileExists{Pattern: "report.md"}.Grade(filesOut(files))
	want := "no file matches report.md; wrote: a.txt, b.txt, c.txt, d.txt, e.txt and 2 more"
	if s.Note != want {
		t.Fatalf("note = %q", s.Note)
	}
}

// TestFileContainsChecksFileText checks the file_contains grader.
// Each row asserts it passes only when one matched file holds every text, ignoring case, that
// the value is the best file's share of texts, and that the note says which texts are missing
// from which file, or that no file matched, or that no files were written.
func TestFileContainsChecksFileText(t *testing.T) {
	g := FileContains{Pattern: "*.md", Items: []string{"Revenue", "2025"}}
	tests := []struct {
		name  string
		files map[string]string
		pass  bool
		value float64
		note  string
	}{
		{"all text in one file", map[string]string{"r.md": "REVENUE in 2025"}, true, 1, ""},
		{"split across files", map[string]string{"a.md": "revenue", "b.md": "2025"}, false, 0.5, "a.md missing: 2025; b.md missing: Revenue"},
		{"one file has it all", map[string]string{"a.md": "revenue", "b.md": "2025 revenue"}, true, 1, ""},
		{"no matching file", map[string]string{"r.txt": "revenue 2025"}, false, 0, "no file matches *.md; wrote: r.txt"},
		{"no files", nil, false, 0, "the target wrote no files"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := g.Grade(filesOut(tc.files))
			if s.Grader != "file_contains" || s.Pass != tc.pass || s.Value != tc.value || s.Note != tc.note {
				t.Fatalf("got %+v", s)
			}
		})
	}
}

// TestFileChecksReportUnreadableFiles checks a failed read of the target's files is not blamed on the target.
// It asserts both graders say they could not read the files, with the error, when nothing matched and the
// read failed, even if some files were read, and still pass when a matching file was read.
func TestFileChecksReportUnreadableFiles(t *testing.T) {
	out := filesOut(nil)
	out.Trace.FilesErr = "walk failed"
	want := "could not read the target's files: walk failed"
	if s := (FileExists{Pattern: "report.md"}).Grade(out); s.Pass || s.Note != want {
		t.Errorf("file_exists: got %+v", s)
	}
	if s := (FileContains{Pattern: "report.md", Items: []string{"x"}}).Grade(out); s.Pass || s.Note != want {
		t.Errorf("file_contains: got %+v", s)
	}

	out = filesOut(map[string]string{"a.txt": "x"})
	out.Trace.FilesErr = "bad.md: denied"
	if s := (FileExists{Pattern: "*.md"}).Grade(out); s.Pass || s.Note != "could not read the target's files: bad.md: denied" {
		t.Errorf("partial: got %+v", s)
	}
	out.Files["report.md"] = []byte("x")
	if s := (FileExists{Pattern: "*.md"}).Grade(out); !s.Pass {
		t.Errorf("matched: got %+v", s)
	}
}
