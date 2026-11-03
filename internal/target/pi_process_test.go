//go:build unix

package target

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestPiKillsTheWholeProcessGroupAtTheTimeout checks a stuck pi and its children are stopped.
// It asserts Run returns about when the context ends (within a second, so the kill works without the wait delay), reports the
// deadline, and the sleeping child pi started is gone.
func TestPiKillsTheWholeProcessGroupAtTheTimeout(t *testing.T) {
	bin, work := fakePi(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	_, err := Pi{Bin: bin, Model: "ollama/m"}.Run(ctx, Input{Prompt: "hang", Dir: work})
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
	if elapsed > 2*time.Second+time.Second {
		t.Errorf("Run took %v", elapsed)
	}
	b, readErr := os.ReadFile(filepath.Join(work, "child.pid"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	var pid int
	if _, scanErr := fmt.Sscan(string(b), &pid); scanErr != nil {
		t.Fatal(scanErr)
	}
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Errorf("child %d is still running", pid)
	}
}
