//go:build unix

package target

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sandboxPiScript = `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "0.99.1"
  exit 0
fi
listing=$(ls .. | tr '\n' ',')
echo '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"pwd='"$PWD"' ls='"$listing"'"}],"stopReason":"stop"}}'
echo '{"type":"agent_end","messages":[]}'
`

func sandboxPi(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pi")
	if err := os.WriteFile(bin, []byte(sandboxPiScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// TestPiRunsInAFreshSandboxEndToEnd checks a real pi process in the real sandbox.
// It asserts pwd is <root>/work, ls .. shows only skills and work, and the root is gone after Run.
func TestPiRunsInAFreshSandboxEndToEnd(t *testing.T) {
	sandboxTempDir(t)
	fixture := t.TempDir()
	writeTree(t, fixture, map[string]string{"a.txt": "a"})

	out, err := Pi{Bin: sandboxPi(t), Model: "ollama/m"}.Run(context.Background(), Input{Prompt: "q", Dir: fixture})
	if err != nil {
		t.Fatal(err)
	}

	root := out.Trace.Sandbox
	want := "pwd=" + filepath.Join(root, "work") + " ls=skills,work,"
	if root == "" || out.Answer != want {
		t.Errorf("answer = %q, want %q", out.Answer, want)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("root %q still there, err = %v", root, err)
	}
}

// TestPiUsesANewSandboxForEachTry checks a retry cannot see an earlier try.
// It asserts two Run calls report two different sandbox roots and both are gone afterwards.
func TestPiUsesANewSandboxForEachTry(t *testing.T) {
	sandboxTempDir(t)
	p := Pi{Bin: sandboxPi(t), Model: "ollama/m"}

	first, err := p.Run(context.Background(), Input{Prompt: "q"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Run(context.Background(), Input{Prompt: "q"})
	if err != nil {
		t.Fatal(err)
	}

	if first.Trace.Sandbox == "" || first.Trace.Sandbox == second.Trace.Sandbox {
		t.Errorf("roots = %q, %q", first.Trace.Sandbox, second.Trace.Sandbox)
	}
	for _, root := range []string{first.Trace.Sandbox, second.Trace.Sandbox} {
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Errorf("root %q still there, err = %v", root, err)
		}
	}
}
