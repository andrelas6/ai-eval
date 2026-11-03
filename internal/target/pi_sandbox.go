package target

import (
	"fmt"
	"os"
	"path/filepath"
)

func makeSandbox(fixture string, skills []string) (box Box, err error) {
	seen := map[string]bool{}
	for _, skill := range skills {
		name := filepath.Base(skill)
		if seen[name] {
			return Box{}, fmt.Errorf("two skills named %q", name)
		}
		seen[name] = true
	}

	made, err := os.MkdirTemp("", "aieval-*")
	if err != nil {
		return Box{}, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(made)
		}
	}()
	root, err := filepath.EvalSymlinks(made)
	if err != nil {
		return Box{}, err
	}

	work := filepath.Join(root, "work")
	if fixture == "" {
		err = os.Mkdir(work, 0o755)
	} else {
		err = os.CopyFS(work, os.DirFS(fixture))
	}
	if err != nil {
		return Box{}, err
	}

	if err = os.Mkdir(filepath.Join(root, "skills"), 0o755); err != nil {
		return Box{}, err
	}
	copies := make([]string, len(skills))
	for i, skill := range skills {
		copies[i] = filepath.Join(root, "skills", filepath.Base(skill))
		if err = os.CopyFS(copies[i], os.DirFS(skill)); err != nil {
			return Box{}, err
		}
	}
	return Box{Root: root, Work: work, Skills: copies, Remove: func() error { return os.RemoveAll(root) }}, nil
}
