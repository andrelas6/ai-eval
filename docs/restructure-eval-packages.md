# ae-plan: Restructure ai-eval into targets, tools, graders and YAML cases

> **Status:** In progress (PR A in review)
> **Last updated:** 2026-10-08

## What's being built

The current single-folder harness gets split into `cmd/aieval` plus `internal/{ollama,target,tool,grader,eval,report}`. Test cases move out of Go code into data files under `evals/*.yaml`, read by a small YAML parser we write ourselves. Skills live in `skills/` and run on top of a model. The model always has to be named: there's no default. Behaviour stays the same as today; the code just gets organized so new models, skills, tools and graders can plug in.

## Constraints

- Standard library only, with no new dependencies. The YAML parser only handles the format our case files use.
- Every target returns a **trace** (tool calls, time per turn, tokens), not just the final text.
- A skill wraps a model, and the model has to be named explicitly. If `-models` is missing, the run fails. A skill target with no model is an error.
- Deferred: running cases in parallel, an LLM judge, other providers.
- Red/Green TDD. The Ollama target is tested against a fake server (`net/http/httptest`), not the real box. Running against the real Ollama server is a manual check, because macOS Local Network permission blocks programs the Claude app starts (see the README).

## What I found in the codebase

- `ollama.go` is the HTTP client and its types. It moves to `internal/ollama` almost as-is.
- `main.go` `runOnce` holds the tool-call loop (up to 4 turns). That becomes the Ollama target's `Run`. Fixed bug: `wall_ns` was always 0 because of `defer` with an unnamed return value. Trace times should be taken directly, not in a `defer`.
- `tool.go` has the `list_dir` schema, `runListDir` (stays inside the fixture folder) and `fixtureNames`. It moves to `internal/tool`. `fixtureNames` moves to the `files` grader.
- `score.go` mixes recall, made-up filenames and pass into one score. It splits into the `files` grader. `tool_called` and `latency` become their own graders.
- `main.go` `printTable`, `percentile` and `writeJSON` move to `internal/report`.
- `testdata/sample/` moves to `fixtures/sample/`.
- Tests use table tests. Same style everywhere.

## What the docs say

- **Go module layout** ([go.dev/doc/modules/layout](https://go.dev/doc/modules/layout)): put commands under `cmd/<prog>/main.go` and shared code under `internal/`, which other modules can't import.
- **Ollama chat API** ([docs/api.md](https://github.com/ollama/ollama/blob/main/docs/api.md)): `tools[]` holds `{type:"function", function:{name, description, parameters}}`. The response returns `message.tool_calls[].function.{name, arguments}`. Tool results go back as `{role:"tool", content, tool_name}`. Durations are in nanoseconds.

## Approach

Four interfaces, each in its own package:

```go
type Target interface { Name() string; Run(ctx, Input) (Output, error) }
type Input  struct { System, Prompt string; Tools []tool.Tool }
type Output struct { Answer string; Trace Trace }
type Trace  struct { Turns []Turn; Wall time.Duration }

type Spec struct { Name, Description string; Parameters map[string]any }
type Tool interface { Spec() Spec; Call(ctx, args map[string]any) (string, error) }

type Grader interface { Name() string; Grade(target.Output) Score }

type Case struct { ID, Prompt, Fixture string; Tools []string; Graders []GraderSpec; Runs int }
```

- `target.Ollama{Client, Model}` runs the tool loop. `target.Skill{Name, Instructions, Model target.Target}` adds the skill text to the system prompt, then calls the model it wraps. Its name looks like `ae-review@qwen3-coder:30b`.
- The YAML parser turns text into `map[string]any` / `[]any` / `string`, and `eval.LoadCase` turns that into a `Case`. Supported: nested maps and lists set by 2-space indentation, `[a, b]` lists, flat `{ k: v }` maps, `#` comments, quoted strings. Anything else (tabs, `|` text blocks, anchors) stops with an error that gives the line number.
- `eval.Runner` runs every target against every case, N times each. It does one warm-up per target, runs the graders after each run, and returns the results.
- CLI: `aieval -models qwen3.5:9b,gpt-oss:20b [-skill skills/ae-review] evals/list-files.yaml`. `-models all` is an explicit choice that expands to everything from `/api/tags`.

## Red / Green

- YAML: parses `evals/list-files.yaml` into the expected tree. Covers nested map, block list, inline list, inline map, comments, quoted value containing `#`. A tab or `|` gives an error with the line number.
- Case: loads a valid case. Missing `prompt`, missing `fixture`, an unknown grader or an unknown tool each give a clear error. `runs` falls back to 1.
- Ollama target (fake server): a plain answer gives 1 turn. tool call → tool result → answer gives 2 turns, with the call recorded in the trace. No final answer after 4 turns gives an error. An HTTP 500 shows up in the error message. Trace `Wall` > 0.
- Skill target: the system prompt contains the skill text. A nil model gives an error. The name is `skill@model`.
- Tool `list_dir`: lists the fixture and refuses `..`, `/etc` and `src/../../`. An unknown tool name sends an error string back to the model, and the run doesn't crash.
- Graders: `files` keeps today's score tests. `tool_called` passes or fails based on the trace. `latency` fails when the run takes longer than `max`.
- Runner (fake target): 2 targets × 1 case × 3 runs gives 6 results with grader scores. A target error is recorded and the loop continues.
- CLI: running without `-models` fails with a message.
- Report: the table sorts by pass count, then median time. The JSON round-trips.

## Most important file changes

- `cmd/aieval/main.go`: new
- `internal/ollama/client.go`: moved from `ollama.go`
- `internal/target/{target.go,ollama.go,skill.go}`: new
- `internal/tool/{tool.go,listdir.go}`: moved from `tool.go`
- `internal/grader/{grader.go,files.go,toolcall.go,latency.go}`
- `internal/eval/{yaml.go,case.go,runner.go}`: new
- `internal/report/{table.go,json.go}`: moved from `main.go`
- `evals/list-files.yaml`, `fixtures/sample/`, `skills/.gitkeep`
- Deleted: root `main.go`, `ollama.go`, `tool.go` and `score.go`, plus their tests and `testdata/`
- `README.md`: new commands and layout

## Changes made while building

- **Tools don't depend on Ollama.** `tool.Tool.Spec()` returns a neutral `tool.Spec`. Only `target/ollama.go` converts it to Ollama's format, so the `ollama` package stays a plain provider client.
- **Graders don't take a `Case`.** `eval` imports `grader`, so `Grade(Case, Output)` would be an import cycle. Each grader gets its settings when it's created (`Files{fixture}`, `ToolCalled{Tool}`, `Latency{Max}`) and grades with `Grade(target.Output)`. The case loader builds graders from the YAML.
- **`tool_called` is not wired into `main.go` yet**, to keep PR A a pure refactor. Cases pick it in YAML.
- **No `git init` subtask.** The repo already existed, so subtasks are numbered from the client move.

## Open questions

- None blocking. Skill format for now: `skills/<name>/SKILL.md`, with the whole file used as instructions.

## Subtasks

Each item is one commit.

1. ✅ Move the client to `internal/ollama`. The root code imports it and tests stay green. (#1)
2. ✅ `internal/tool`: `Tool` interface, registry and `list_dir`. (#2)
3. ✅ `internal/target`: `Target`, `Output` and `Trace`, plus the Ollama target with the tool loop, tested against a fake server. (#3)
4. ✅ `internal/target/skill.go`: skill wraps an explicit model. A nil model gives an error. (#4)
5. 🔍 `internal/grader`: interface plus `files`, `tool_called` and `latency`. (#5, in review)
6. `internal/eval/yaml.go`: the minimal parser and its tests.
7. `internal/eval/case.go`: load and check a `Case`. Add `evals/list-files.yaml` and move the fixture to `fixtures/sample/`.
8. `internal/eval/runner.go`: targets × cases × runs, warm-up, graders.
9. `internal/report`: table and JSON.
10. `cmd/aieval`: wire everything together, require `-models`, delete the old root files, update the README, test once by hand against the real Ollama box.
11. **Size check:** about 1,100–1,300 lines changed, over 500. Split:
    - **PR A:** subtasks 1–5 (move the code and add the interfaces, behaviour unchanged)
    - **PR B:** subtasks 6–7 (YAML cases)
    - **PR C:** subtasks 8–10 (runner, report, CLI)