package main

import (
	"ai-eval/internal/grader"
	"ai-eval/internal/ollama"
	"ai-eval/internal/target"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRunOnceRecordsWallTime guards the bug where every run reported 0 seconds.
// The fake server waits 20ms, so it asserts the recorded run time is at least 20ms.
func TestRunOnceRecordsWallTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.Write([]byte(`{"message":{"role":"assistant","content":"README.md"}}`))
	}))
	defer srv.Close()

	c := &ollama.Client{Host: srv.URL, HTTP: srv.Client()}
	r := runOnce(target.Ollama{Client: c, Model: "fake"}, "fixtures/sample", time.Second, nil)

	if r.Err != "" {
		t.Fatalf("unexpected error: %s", r.Err)
	}
	if r.Wall < 20*time.Millisecond {
		t.Fatalf("Wall = %v, want >= 20ms", r.Wall)
	}
}

// TestRunOncePassNeedsEveryGrader checks how a run's pass is decided.
// It asserts every grader's score is kept on the run, and one failing grader (here files,
// because main.go is never named) makes the whole run fail even though latency passed.
func TestRunOncePassNeedsEveryGrader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"role":"assistant","content":"README.md"}}`))
	}))
	defer srv.Close()

	c := &ollama.Client{Host: srv.URL, HTTP: srv.Client()}
	files := grader.Files{Expected: []string{"README.md", "main.go"}, Known: []string{"README.md", "main.go"}}
	r := runOnce(target.Ollama{Client: c, Model: "fake"}, "fixtures/sample", time.Second, []grader.Grader{files, grader.Latency{}})

	if len(r.Scores) != 2 || r.Pass {
		t.Fatalf("pass = %v, scores = %+v", r.Pass, r.Scores)
	}
	if got := r.metric("files", "recall"); got != 0.5 {
		t.Errorf("recall = %v", got)
	}
}
