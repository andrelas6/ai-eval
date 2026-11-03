package target

import (
	"errors"
	"fmt"
	"io/fs"
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
	return Box{
		Root:     root,
		Work:     work,
		Skills:   copies,
		Remove:   func() error { return os.RemoveAll(root) },
		NewFiles: func() (map[string][]byte, error) { return newFiles(fixture, work) },
	}, nil
}

func newFiles(fixture, work string) (map[string][]byte, error) {
	fixtureFiles := map[string]bool{}
	if fixture != "" {
		err := fs.WalkDir(os.DirFS(fixture), ".", func(name string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				fixtureFiles[name] = true
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}

	root, err := os.OpenRoot(work)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	var files map[string][]byte
	var skipped []error
	walkErr := fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			skipped = append(skipped, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || fixtureFiles[name] {
			return nil
		}
		content, err := root.ReadFile(name)
		if err != nil {
			skipped = append(skipped, err)
			return nil
		}
		if files == nil {
			files = map[string][]byte{}
		}
		files[name] = content
		return nil
	})
	if walkErr != nil {
		skipped = append(skipped, walkErr)
	}
	return files, errors.Join(skipped...)
}
