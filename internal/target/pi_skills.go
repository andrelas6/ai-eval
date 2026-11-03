package target

import (
	"path/filepath"
	"strings"
)

func checkSkillsLoaded(calls []ToolCall, skills []string, workDir string) (offered, loaded []string, err error) {
	var missing []string
	for _, dir := range skills {
		name := filepath.Base(dir)
		offered = append(offered, name)
		if skillWasRead(calls, filepath.Join(dir, "SKILL.md"), workDir) {
			loaded = append(loaded, name)
		} else {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		err = RetryError{Reason: "skill not loaded: " + strings.Join(missing, ", ")}
	}
	return offered, loaded, err
}

func skillWasRead(calls []ToolCall, skillFile, workDir string) bool {
	for _, c := range calls {
		if c.Err != "" {
			continue
		}
		switch c.Name {
		case "read":
			path, _ := c.Args["path"].(string)
			if path == "" {
				continue
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(workDir, path)
			}
			if filepath.Clean(path) == skillFile {
				return true
			}
		case "bash":
			command, _ := c.Args["command"].(string)
			if strings.Contains(command, skillFile) {
				return true
			}
		}
	}
	return false
}
