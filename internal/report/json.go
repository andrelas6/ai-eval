package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"ai-eval/internal/eval"
)

const TimeFormat = "20060102-150405"

type resultsFile struct {
	Results []eval.Result `json:"results"`
	Grid    Grid          `json:"grid"`
}

func WriteJSON(dir string, results []eval.Result, grid Grid, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, now.Format(TimeFormat)+".json")
	b, err := json.MarshalIndent(resultsFile{Results: results, Grid: grid}, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o644)
}
