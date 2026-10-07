package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestChat checks a chat request goes out and the reply is read back correctly.
// It asserts the request hits POST /api/chat with streaming off, and the reply's tool calls
// and token count are decoded.
func TestChat(t *testing.T) {
	var got ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"list_dir","arguments":{"path":"."}}}]},"eval_count":10,"eval_duration":1000000000}`))
	}))
	defer srv.Close()

	c := &Client{Host: srv.URL, HTTP: srv.Client()}
	resp, err := c.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "m" || got.Stream {
		t.Errorf("request sent = %+v", got)
	}
	if len(resp.Message.ToolCalls) != 1 || resp.Message.ToolCalls[0].Function.Arguments["path"] != "." {
		t.Errorf("tool calls = %+v", resp.Message.ToolCalls)
	}
	if resp.EvalCount != 10 {
		t.Errorf("eval_count = %d", resp.EvalCount)
	}
}

// TestListModels checks the model names are read from /api/tags in order.
func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[{"name":"a"},{"name":"b"}]}`))
	}))
	defer srv.Close()

	c := &Client{Host: srv.URL, HTTP: srv.Client()}
	names, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "a,b" {
		t.Errorf("names = %v", names)
	}
}

// TestErrorStatusShowsBody checks a non-200 reply turns into an error.
// It asserts the server's message (e.g. "model does not support tools") is part of that error.
func TestErrorStatusShowsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model does not support tools", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := &Client{Host: srv.URL, HTTP: srv.Client()}
	_, err := c.Chat(context.Background(), ChatRequest{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "does not support tools") {
		t.Fatalf("err = %v", err)
	}
}
