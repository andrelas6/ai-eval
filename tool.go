package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// listDirTool is the tool schema sent to Ollama.
var listDirTool = Tool{
	Type: "function",
	Function: ToolFunction{
		Name:        "list_dir",
		Description: "List the files and folders in a directory. Folders end with '/'.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Directory to list, relative to the current directory. Use \".\" for the current directory.",
				},
			},
			"required": []string{"path"},
		},
	},
}

// runListDir lists path inside root, one entry per line. Paths that leave root are rejected.
func runListDir(root, path string) (string, error) {
	if path == "" {
		path = "."
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", path)
	}
	rel := filepath.Clean(path)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("path escapes the current directory: %s", path)
	}

	entries, err := os.ReadDir(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() {
			n += "/"
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, "\n"), nil
}

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
