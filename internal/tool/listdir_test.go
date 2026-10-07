package tool

import (
	"context"
	"strings"
	"testing"
)

const sample = "../../testdata/sample"

// TestListDirSpec checks how list_dir describes itself to a model.
// It asserts the name is "list_dir", there is a description, and the parameters are a JSON schema object.
func TestListDirSpec(t *testing.T) {
	spec := ListDir{Root: sample}.Spec()
	if spec.Name != "list_dir" || spec.Description == "" || spec.Parameters["type"] != "object" {
		t.Fatalf("spec = %+v", spec)
	}
}

// TestListDirCall checks list_dir lists the fixture folder.
// It asserts "." shows files, hidden files and folders (with a trailing "/"), a missing path
// lists the root, and a subfolder lists only its own files.
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

// TestListDirRejectsPathsOutsideRoot checks the model can't read outside the fixture folder.
// It asserts "..", absolute paths and sneaky paths like "src/../../" all return an error.
func TestListDirRejectsPathsOutsideRoot(t *testing.T) {
	ld := ListDir{Root: sample}
	for _, bad := range []string{"..", "../..", "/etc", "src/../../"} {
		if _, err := ld.Call(context.Background(), map[string]any{"path": bad}); err == nil {
			t.Errorf("path %q should be rejected", bad)
		}
	}
}
