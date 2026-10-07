package main

import (
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
	r := runOnce(target.Ollama{Client: c, Model: "fake"}, "testdata/sample", time.Second)

	if r.Err != "" {
		t.Fatalf("unexpected error: %s", r.Err)
	}
	if r.Wall < 20*time.Millisecond {
		t.Fatalf("Wall = %v, want >= 20ms", r.Wall)
	}
}
