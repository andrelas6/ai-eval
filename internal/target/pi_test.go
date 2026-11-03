package target

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type processCall struct {
	bin  string
	args []string
	dir  string
}

func fakeProcess(calls *[]processCall, stdout []byte, err error) func(ctx context.Context, bin string, args []string, dir string) ([]byte, []byte, error) {
	return func(ctx context.Context, bin string, args []string, dir string) ([]byte, []byte, error) {
		*calls = append(*calls, processCall{bin, args, dir})
		return stdout, nil, err
	}
}

func recording(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

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

// TestPiBuildsThePinnedArguments checks the command line handed to pi.
// It asserts the pinned flags, the provider and model split at the first slash, one --skill per
// skill, the appended system prompt, the prompt last, the binary name and the working folder.
func TestPiBuildsThePinnedArguments(t *testing.T) {
	var calls []processCall
	p := Pi{
		Bin:     "pi",
		Model:   "openrouter/deepseek/deepseek-v4-flash",
		Skills:  []string{"/s/skills/a", "/s/skills/b"},
		Process: fakeProcess(&calls, recording(t, "pi-skill-read.jsonl"), nil),
	}

	if _, err := p.Run(context.Background(), Input{System: "be brief", Prompt: "question?", Dir: "/work"}); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"-p", "--mode", "json", "--no-session", "--no-skills", "--no-context-files", "--no-extensions",
		"--no-prompt-templates", "--no-approve", "--tools", "read,bash", "--thinking", "medium",
		"--provider", "openrouter", "--model", "deepseek/deepseek-v4-flash",
		"--skill", "/s/skills/a", "--skill", "/s/skills/b",
		"--append-system-prompt", "be brief", "question?",
	}
	if len(calls) != 1 || calls[0].bin != "pi" || calls[0].dir != "/work" || !slices.Equal(calls[0].args, want) {
		t.Fatalf("call = %+v\nwant args %v", calls, want)
	}
}

// TestPiLeavesOutWhatIsNotSet checks optional arguments.
// It asserts no --skill and no --append-system-prompt appear without skills or a system prompt,
// and that a chosen thinking level replaces the medium default.
func TestPiLeavesOutWhatIsNotSet(t *testing.T) {
	var calls []processCall
	p := Pi{Bin: "pi", Model: "ollama/gpt-oss:20b", Thinking: "high", Process: fakeProcess(&calls, recording(t, "pi-skill-read.jsonl"), nil)}

	if _, err := p.Run(context.Background(), Input{Prompt: "q"}); err != nil {
		t.Fatal(err)
	}

	args := calls[0].args
	if slices.Contains(args, "--skill") || slices.Contains(args, "--append-system-prompt") {
		t.Errorf("args = %v", args)
	}
	i := slices.Index(args, "--thinking")
	if args[i+1] != "high" || !slices.Equal(args[len(args)-5:], []string{"--provider", "ollama", "--model", "gpt-oss:20b", "q"}) {
		t.Errorf("args = %v", args)
	}
}

// TestPiNeedsAProviderInTheModel checks the model must be written provider/id.
// It asserts a model with no slash is an error and pi is never started.
func TestPiNeedsAProviderInTheModel(t *testing.T) {
	var calls []processCall
	p := Pi{Model: "gpt-oss:20b", Process: fakeProcess(&calls, nil, nil)}

	_, err := p.Run(context.Background(), Input{Prompt: "q"})

	if err == nil || !strings.Contains(err.Error(), "provider/model") || len(calls) != 0 {
		t.Fatalf("err = %v, calls = %d", err, len(calls))
	}
}

// TestPiReturnsProcessErrors checks a failing process.
// It asserts the process error is returned as it is.
func TestPiReturnsProcessErrors(t *testing.T) {
	var calls []processCall
	p := Pi{Model: "ollama/m", Process: fakeProcess(&calls, nil, errors.New("exit status 2"))}

	_, err := p.Run(context.Background(), Input{Prompt: "q"})

	if err == nil || !strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("err = %v", err)
	}
}

// TestPiParsesARecordedRun checks the events of a real run that read a skill.
// It asserts the answer is the final assistant text, there is one turn per assistant message,
// the first turn holds the read call with its path and result, and tokens and cost are summed.
func TestPiParsesARecordedRun(t *testing.T) {
	var calls []processCall
	p := Pi{Model: "openrouter/m", Process: fakeProcess(&calls, recording(t, "pi-skill-read.jsonl"), nil)}

	out, err := p.Run(context.Background(), Input{Prompt: "q"})
	if err != nil {
		t.Fatal(err)
	}

	if out.Answer != "MARGIN-SKILL: 15%\n\n(30 / 200 = 0.15)" {
		t.Errorf("answer = %q", out.Answer)
	}
	if len(out.Trace.Turns) != 2 {
		t.Fatalf("turns = %d", len(out.Trace.Turns))
	}
	read := out.Trace.Turns[0].ToolCalls
	if len(read) != 1 || read[0].Name != "read" || !strings.HasSuffix(read[0].Args["path"].(string), "margin-calc/SKILL.md") {
		t.Fatalf("calls = %+v", read)
	}
	if !strings.Contains(read[0].Result, "Operating margin = operating income / revenue") || read[0].Err != "" {
		t.Errorf("result = %q, err = %q", read[0].Result, read[0].Err)
	}
	if len(out.Trace.Turns[1].ToolCalls) != 0 {
		t.Errorf("second turn calls = %+v", out.Trace.Turns[1].ToolCalls)
	}
	if out.Trace.Tokens != 1899+1990 {
		t.Errorf("tokens = %d", out.Trace.Tokens)
	}
	if math.Abs(out.Trace.Cost-(0.000181818+0.0000526344)) > 1e-9 {
		t.Errorf("cost = %v", out.Trace.Cost)
	}
	if out.Trace.Wall <= 0 {
		t.Errorf("wall = %v", out.Trace.Wall)
	}
}

// TestPiTurnsAStopReasonErrorIntoAnError checks a run pi reports as failed while exiting 0.
// It asserts the recorded bad-model run gives an error holding the errorMessage.
func TestPiTurnsAStopReasonErrorIntoAnError(t *testing.T) {
	var calls []processCall
	p := Pi{Model: "openrouter/does-not-exist/x", Process: fakeProcess(&calls, recording(t, "pi-bad-model.jsonl"), nil)}

	_, err := p.Run(context.Background(), Input{Prompt: "q"})

	if err == nil || !strings.Contains(err.Error(), "does-not-exist/x is not a valid model ID") {
		t.Fatalf("err = %v", err)
	}
}

// TestPiNeedsAnAgentEnd checks a cut-off stream.
// It asserts output without agent_end is an error, not an empty answer.
func TestPiNeedsAnAgentEnd(t *testing.T) {
	cut := strings.Split(string(recording(t, "pi-skill-read.jsonl")), "\n")
	var kept []string
	for _, line := range cut {
		if !strings.Contains(line, `"type":"agent_end"`) {
			kept = append(kept, line)
		}
	}
	var calls []processCall
	p := Pi{Model: "openrouter/m", Process: fakeProcess(&calls, []byte(strings.Join(kept, "\n")), nil)}

	_, err := p.Run(context.Background(), Input{Prompt: "q"})

	if err == nil || !strings.Contains(err.Error(), "agent_end") {
		t.Fatalf("err = %v", err)
	}
}

// TestPiKeepsToolErrors checks a tool that failed.
// It asserts a tool_execution_end with isError puts the text in the call's Err, not Result.
func TestPiKeepsToolErrors(t *testing.T) {
	stream := `{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","id":"c1","name":"bash","arguments":{"command":"false"}}],"usage":{"totalTokens":5,"cost":{"total":0.5}},"stopReason":"toolUse"}}
{"type":"tool_execution_end","toolCallId":"c1","toolName":"bash","result":{"content":[{"type":"text","text":"exit 1"}]},"isError":true}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"failed"}],"usage":{"totalTokens":5,"cost":{"total":0.5}},"stopReason":"stop"}}
{"type":"agent_end","messages":[]}
`
	var calls []processCall
	p := Pi{Model: "x/m", Process: fakeProcess(&calls, []byte(stream), nil)}

	out, err := p.Run(context.Background(), Input{Prompt: "q"})
	if err != nil {
		t.Fatal(err)
	}
	c := out.Trace.Turns[0].ToolCalls[0]
	if c.Name != "bash" || c.Err != "exit 1" || c.Result != "" || out.Trace.Tokens != 10 || out.Trace.Cost != 1 {
		t.Errorf("call = %+v, trace = %+v", c, out.Trace)
	}
}

// TestPiTreatsOddStopReasonsAsErrors checks a run that ended for a reason other than stop or toolUse.
// It asserts aborted and length give "pi run stopped: <reason>", and stop is fine.
func TestPiTreatsOddStopReasonsAsErrors(t *testing.T) {
	for _, reason := range []string{"aborted", "length"} {
		stream := `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"cut"}],"stopReason":"` + reason + `"}}
{"type":"agent_end","messages":[]}
`
		var calls []processCall
		p := Pi{Model: "x/m", Process: fakeProcess(&calls, []byte(stream), nil)}

		_, err := p.Run(context.Background(), Input{Prompt: "q"})

		if err == nil || err.Error() != "pi run stopped: "+reason {
			t.Errorf("%s: err = %v", reason, err)
		}
	}
}

// TestPiKeepsToolResultsItCannotMatch checks a tool result with an unknown call id.
// It asserts the result is added to the last turn as its own call, with its tool name.
func TestPiKeepsToolResultsItCannotMatch(t *testing.T) {
	stream := `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"stopReason":"stop"}}
{"type":"tool_execution_end","toolCallId":"nobody","toolName":"bash","result":{"content":[{"type":"text","text":"late"}]},"isError":false}
{"type":"agent_end","messages":[]}
`
	var calls []processCall
	p := Pi{Model: "x/m", Process: fakeProcess(&calls, []byte(stream), nil)}

	out, err := p.Run(context.Background(), Input{Prompt: "q"})
	if err != nil {
		t.Fatal(err)
	}
	got := out.Trace.Turns[len(out.Trace.Turns)-1].ToolCalls
	if len(got) != 1 || got[0].Name != "bash" || got[0].Result != "late" {
		t.Errorf("calls = %+v", got)
	}
}

const fakePiScript = `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "$$" >> "$(dirname "$0")/version-calls"
  echo "0.99.1"
  exit 0
fi
for last; do :; done
case "$last" in
  hang)
    sleep 30 &
    echo $! > "$PWD/child.pid"
    sleep 30
    ;;
  fail)
    i=1
    while [ $i -le 30 ]; do echo "err line $i" >&2; i=$((i+1)); done
    exit 3
    ;;
  stdin)
    cat > /dev/null
    ;;
  badstream)
    echo "stderr says boom" >&2
    echo '{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"model exploded"}}'
    echo '{"type":"agent_end","messages":[]}'
    exit 0
    ;;
esac
echo '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"cwd='"$PWD"' args='"$*"'"}],"stopReason":"stop"}}'
echo '{"type":"agent_end","messages":[]}'
`

func fakePi(t *testing.T) (bin, work string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "pi")
	if err := os.WriteFile(bin, []byte(fakePiScript), 0o755); err != nil {
		t.Fatal(err)
	}
	work = filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	work, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	return bin, work
}

// TestPiRunsTheRealProcess checks the default process against a fake pi script.
// It asserts the script ran in the input's folder, got the pinned flags and the absolute
// --skill path, and the prompt last, and that the version of pi is recorded in Trace.Agent.
func TestPiRunsTheRealProcess(t *testing.T) {
	bin, work := fakePi(t)
	p := Pi{Bin: bin, Model: "ollama/gpt-oss:20b", Skills: []string{"/abs/skills/fin"}}

	out, err := p.Run(context.Background(), Input{Prompt: "go", Dir: work})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"cwd=" + work, "--no-skills", "--provider ollama --model gpt-oss:20b", "--skill /abs/skills/fin", "--tools read,bash"} {
		if !strings.Contains(out.Answer, want) {
			t.Errorf("answer %q is missing %q", out.Answer, want)
		}
	}
	if !strings.HasSuffix(out.Answer, " go") {
		t.Errorf("prompt is not last: %q", out.Answer)
	}
	if out.Trace.Agent != "pi 0.99.1" {
		t.Errorf("agent = %q", out.Trace.Agent)
	}
}

// TestPiReadsTheVersionOnce checks the version lookup is cached.
// It asserts two runs on one target start "pi --version" a single time.
func TestPiReadsTheVersionOnce(t *testing.T) {
	bin, work := fakePi(t)
	p := Pi{Bin: bin, Model: "ollama/m"}

	for range 2 {
		if _, err := p.Run(context.Background(), Input{Prompt: "go", Dir: work}); err != nil {
			t.Fatal(err)
		}
	}

	b, err := os.ReadFile(filepath.Join(filepath.Dir(bin), "version-calls"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n != 1 {
		t.Errorf("version calls = %d", n)
	}
}

// TestPiRunSurvivesAnUnreadableVersion checks a missing version doesn't fail the run.
// It asserts a Bin whose --version fails still returns an answer, with an empty Trace.Agent.
func TestPiRunSurvivesAnUnreadableVersion(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "pi")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then exit 1; fi\necho '{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"ok\"}],\"stopReason\":\"stop\"}}'\necho '{\"type\":\"agent_end\",\"messages\":[]}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := Pi{Bin: bin, Model: "ollama/m"}.Run(context.Background(), Input{Prompt: "go", Dir: dir})

	if err != nil || out.Answer != "ok" || out.Trace.Agent != "" {
		t.Errorf("out = %+v, err = %v", out, err)
	}
}

// TestPiDoesNotHangOnStdin checks pi's stdin is closed.
// It asserts a script that reads stdin until it ends returns at once.
func TestPiDoesNotHangOnStdin(t *testing.T) {
	bin, work := fakePi(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err := Pi{Bin: bin, Model: "ollama/m"}.Run(ctx, Input{Prompt: "stdin", Dir: work})

	if err != nil || time.Since(start) > 2*time.Second {
		t.Errorf("err = %v after %v", err, time.Since(start))
	}
}

// TestPiNonZeroExitCarriesTheStderrTail checks a crashing pi.
// It asserts the error mentions the exit and the last stderr lines, and not the first ones.
func TestPiNonZeroExitCarriesTheStderrTail(t *testing.T) {
	bin, work := fakePi(t)

	_, err := Pi{Bin: bin, Model: "ollama/m"}.Run(context.Background(), Input{Prompt: "fail", Dir: work})

	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "err line 30") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "err line 1\n") || strings.Contains(err.Error(), "err line 2\n") {
		t.Errorf("err keeps too much: %v", err)
	}
}

// TestPiStreamErrorKeepsTheStderrTail checks a failed run that exits 0.
// It asserts the error holds both the errorMessage from the events and pi's stderr.
func TestPiStreamErrorKeepsTheStderrTail(t *testing.T) {
	bin, work := fakePi(t)

	_, err := Pi{Bin: bin, Model: "ollama/m"}.Run(context.Background(), Input{Prompt: "badstream", Dir: work})

	if err == nil || !strings.Contains(err.Error(), "model exploded") || !strings.Contains(err.Error(), "stderr says boom") {
		t.Fatalf("err = %v", err)
	}
}

// TestPiStderrTailKeepsWholeLinesAndCharacters checks how stderr is cut.
// It asserts a long stderr is cut on line boundaries to at most 2KB, and one huge line of
// multi-byte characters is cut without splitting a character.
func TestPiStderrTailKeepsWholeLinesAndCharacters(t *testing.T) {
	var lines []string
	for i := 1; i <= 15; i++ {
		lines = append(lines, fmt.Sprintf("line %02d %s", i, strings.Repeat("x", 190)))
	}
	tail := stderrTail([]byte(strings.Join(lines, "\n")))
	if len(tail) > 2048 || !strings.HasPrefix(tail, "line ") || !strings.Contains(tail, "line 15") {
		t.Errorf("tail starts %q, len %d", tail[:10], len(tail))
	}

	tail = stderrTail([]byte(strings.Repeat("é", 3000)))
	if !utf8.ValidString(tail) || len(tail) > 2048 {
		t.Errorf("valid = %v, len = %d", utf8.ValidString(tail), len(tail))
	}
}

// TestPiCachesAFailedVersionLookup checks a broken pi --version is tried once.
// It asserts two runs against a bin whose --version fails start it for the version only once.
func TestPiCachesAFailedVersionLookup(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "pi")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo x >> \"" + dir + "/calls\"; exit 1; fi\necho '{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"content\":[],\"stopReason\":\"stop\"}}'\necho '{\"type\":\"agent_end\",\"messages\":[]}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := (Pi{Bin: bin, Model: "ollama/m"}).Run(context.Background(), Input{Prompt: "go", Dir: dir}); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if n := strings.Count(string(b), "\n"); n != 1 {
		t.Errorf("version calls = %d", n)
	}
}
