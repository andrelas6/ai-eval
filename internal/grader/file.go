package grader

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"ai-eval/internal/target"
)

const maxListedFiles = 5

type FileExists struct {
	Pattern string
}

func (FileExists) Name() string {
	return "file_exists"
}

func (g FileExists) Grade(out target.Output) Score {
	s := Score{Grader: g.Name()}
	names := slices.Sorted(maps.Keys(out.Files))
	if len(matchFiles(g.Pattern, names)) > 0 {
		s.Value = 1
		s.Pass = true
	} else {
		s.Note = noFileNote(g.Pattern, names, out.Trace.FilesErr)
	}
	return s
}

type FileContains struct {
	Pattern string
	Items   []string
}

func (FileContains) Name() string {
	return "file_contains"
}

func (g FileContains) Grade(out target.Output) Score {
	s := Score{Grader: g.Name()}
	names := slices.Sorted(maps.Keys(out.Files))
	matched := matchFiles(g.Pattern, names)
	if len(matched) == 0 {
		s.Note = noFileNote(g.Pattern, names, out.Trace.FilesErr)
		return s
	}
	var gaps []string
	for _, name := range matched {
		inner := Contains{Items: g.Items}.Grade(target.Output{Answer: string(out.Files[name])})
		s.Value = max(s.Value, inner.Value)
		if inner.Pass {
			s.Pass = true
		} else {
			gaps = append(gaps, name+" "+inner.Note)
		}
	}
	if !s.Pass {
		s.Note = strings.Join(gaps, "; ")
	}
	return s
}

func matchFiles(pattern string, names []string) []string {
	var matched []string
	for _, name := range names {
		if ok, _ := path.Match(pattern, name); ok {
			matched = append(matched, name)
		}
	}
	return matched
}

func noFileNote(pattern string, names []string, filesErr string) string {
	if filesErr != "" {
		return "could not read the target's files: " + filesErr
	}
	if len(names) == 0 {
		return "the target wrote no files"
	}
	note := fmt.Sprintf("no file matches %s; wrote: ", pattern)
	if len(names) > maxListedFiles {
		return note + strings.Join(names[:maxListedFiles], ", ") + fmt.Sprintf(" and %d more", len(names)-maxListedFiles)
	}
	return note + strings.Join(names, ", ")
}
