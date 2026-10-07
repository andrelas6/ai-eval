package tool

import (
	"testing"
)

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

func TestBuildUnknownTool(t *testing.T) {
	_, err := Build([]string{"list_dir", "rm_rf"}, sample)
	if err == nil || err.Error() != `unknown tool "rm_rf"` {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildNoTools(t *testing.T) {
	tools, err := Build(nil, sample)
	if err != nil || len(tools) != 0 {
		t.Fatalf("tools = %v, err = %v", tools, err)
	}
}
