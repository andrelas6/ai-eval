package main

import (
	"ai-eval/internal/ollama"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunOnceRecordsWallTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.Write([]byte(`{"message":{"role":"assistant","content":"README.md"}}`))
	}))
	defer srv.Close()

	c := &ollama.Client{Host: srv.URL, HTTP: srv.Client()}
	r := runOnce(c, "fake", "testdata/sample", time.Second)

	if r.Err != "" {
		t.Fatalf("unexpected error: %s", r.Err)
	}
	if r.Wall < 20*time.Millisecond {
		t.Fatalf("Wall = %v, want >= 20ms", r.Wall)
	}
}
