package tool

import (
	"context"
	"strings"
	"testing"
)

const sample = "../../testdata/sample"

func TestListDirSpec(t *testing.T) {
	spec := ListDir{Root: sample}.Spec()
	if spec.Name != "list_dir" || spec.Description == "" || spec.Parameters["type"] != "object" {
		t.Fatalf("spec = %+v", spec)
	}
}

func TestListDirCall(t *testing.T) {
	ld := ListDir{Root: sample}

	out, err := ld.Call(context.Background(), map[string]any{"path": "."})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"README.md", ".env.example", "src/"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing %q missing %q", out, want)
		}
	}

	if out, err := ld.Call(context.Background(), map[string]any{}); err != nil || !strings.Contains(out, "README.md") {
		t.Errorf("missing path should list the root, got %q, %v", out, err)
	}

	if out, err := ld.Call(context.Background(), map[string]any{"path": "src"}); err != nil || out != "app.go" {
		t.Errorf("subdir = %q, %v", out, err)
	}
}

func TestListDirRejectsPathsOutsideRoot(t *testing.T) {
	ld := ListDir{Root: sample}
	for _, bad := range []string{"..", "../..", "/etc", "src/../../"} {
		if _, err := ld.Call(context.Background(), map[string]any{"path": bad}); err == nil {
			t.Errorf("path %q should be rejected", bad)
		}
	}
}
