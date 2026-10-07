package tool

import (
	"testing"
)

// TestBuild checks tools are looked up by name and pointed at the case's fixture folder.
// It asserts "list_dir" returns a ListDir whose root is the fixture.
func TestBuild(t *testing.T) {
	tools, err := Build([]string{"list_dir"}, sample)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Spec().Name != "list_dir" {
		t.Fatalf("tools = %+v", tools)
	}
	if ld, ok := tools[0].(ListDir); !ok || ld.Root != sample {
		t.Errorf("list_dir should use the fixture as root, got %+v", tools[0])
	}
}

// TestBuildUnknownTool checks a typo or unsupported tool name in a case fails early.
// It asserts the error names the unknown tool.
func TestBuildUnknownTool(t *testing.T) {
	_, err := Build([]string{"list_dir", "rm_rf"}, sample)
	if err == nil || err.Error() != `unknown tool "rm_rf"` {
		t.Fatalf("err = %v", err)
	}
}

// TestBuildNoTools checks a case with no tools is fine and gives an empty list.
func TestBuildNoTools(t *testing.T) {
	tools, err := Build(nil, sample)
	if err != nil || len(tools) != 0 {
		t.Fatalf("tools = %v, err = %v", tools, err)
	}
}
