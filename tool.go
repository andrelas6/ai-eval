package main

import (
	"os"
	"path/filepath"
)

// fixtureNames returns the top-level entry names and every name in the tree.
func fixtureNames(root string) (top, all []string, err error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		top = append(top, e.Name())
	}
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != root {
			all = append(all, d.Name())
		}
		return nil
	})
	return top, all, err
}
