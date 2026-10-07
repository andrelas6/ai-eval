package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ListDir struct {
	Root string
}

func (ListDir) Spec() Spec {
	return Spec{
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
	}
}

func (l ListDir) Call(_ context.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
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

	entries, err := os.ReadDir(filepath.Join(l.Root, rel))
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, "\n"), nil
}
