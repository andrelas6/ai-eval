package main

import (
	"strings"
	"testing"
)

func TestRunListDir(t *testing.T) {
	root := "testdata/sample"

	out, err := runListDir(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"README.md", ".env.example", "src/"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing %q missing %q", out, want)
		}
	}

	if _, err := runListDir(root, "src"); err != nil {
		t.Errorf("subdir should work: %v", err)
	}

	for _, bad := range []string{"..", "../..", "/etc", "src/../../"} {
		if _, err := runListDir(root, bad); err == nil {
			t.Errorf("path %q should be rejected", bad)
		}
	}
}
