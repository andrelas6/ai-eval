package target

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	piWaitDelay     = 3 * time.Second
	stderrTailMax   = 2048
	stderrTailLines = 20
)

var piVersions sync.Map

func withStderrTail(err error, stderr []byte) error {
	tail := stderrTail(stderr)
	if tail == "" {
		return err
	}
	return fmt.Errorf("%w\nstderr:\n%s", err, tail)
}

func stderrTail(stderr []byte) string {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	if len(lines) > stderrTailLines {
		lines = lines[len(lines)-stderrTailLines:]
	}
	text := strings.Join(lines, "\n")
	for len(text) > stderrTailMax && len(lines) > 1 {
		lines = lines[1:]
		text = strings.Join(lines, "\n")
	}
	if len(text) > stderrTailMax {
		text = text[len(text)-stderrTailMax:]
		for !utf8.RuneStart(text[0]) {
			text = text[1:]
		}
	}
	return strings.TrimSpace(text)
}

func piVersion(ctx context.Context, bin string) string {
	if v, ok := piVersions.Load(bin); ok {
		return v.(string)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stdout, _, err := execProcess(ctx, bin, []string{"--version"}, os.TempDir())
	version := strings.TrimSpace(string(stdout))
	if err != nil || version == "" {
		piVersions.Store(bin, "")
		return ""
	}
	if !strings.HasPrefix(version, "pi ") {
		version = "pi " + version
	}
	piVersions.Store(bin, version)
	return version
}
