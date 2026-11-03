package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"ai-eval/internal/eval"
)

func WriteJSON(dir string, results []eval.Result, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, now.Format("20060102-150405")+".json")
	b, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o644)
}
