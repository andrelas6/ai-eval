package target

import (
	"context"
	"strings"
	"testing"
)

// TestPiName checks how a Pi target is named in the table.
// It asserts no skills gives pi@model and skills give pi+a,b@model using the folder names.
func TestPiName(t *testing.T) {
	tests := []struct {
		pi   Pi
		want string
	}{
		{Pi{Model: "ollama/gpt-oss:20b"}, "pi@ollama/gpt-oss:20b"},
		{Pi{Model: "m", Skills: []string{"/s/skills/a", "/s/skills/b"}}, "pi+a,b@m"},
	}
	for _, tc := range tests {
		if got := tc.pi.Name(); got != tc.want {
			t.Errorf("Name() = %q, want %q", got, tc.want)
		}
	}
}

// TestPiPlaceholderRefusesToRun checks the placeholder until the real Pi target lands.
// It asserts Run returns the error "pi target not built yet".
func TestPiPlaceholderRefusesToRun(t *testing.T) {
	_, err := Pi{Model: "m"}.Run(context.Background(), Input{Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "pi target not built yet") {
		t.Fatalf("err = %v", err)
	}
}
