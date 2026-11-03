# ai-eval

A small eval for local Ollama models. Each model gets the prompt **"list the files of this directory"** and a `list_dir` tool. Each answer is scored on:

- **Content**: did it name the real files and avoid made-up ones?
- **Speed**: how long did it take, from the first request to the final answer?

## Requirements

- Go 1.26+
- An Ollama server you can reach (default `http://192.168.2.156:11434`) with models that support tool calling

## Run

Quick check with one model:

```bash
go run ./cmd/aieval -models qwen3.5:9b evals/list-files
```

Several models, each run through every case in the suite folder:

```bash
go run ./cmd/aieval -models gpt-oss:20b,qwen3-coder:30b evals/list-files
```

Unit tests (scoring and the tool):

```bash
go test ./...
```

### Flags

| Flag | Default | What it does |
|---|---|---|
| `-agent` | `ollama` | `ollama` or `pi`. `pi` is not built yet |
| `-models` | *(required)* | Comma-separated model names |
| `-skills` | *(none)* | A skill set: `none`, or names joined with commas. Repeat the flag for more sets. Names map to `<suite>/skills/<name>/SKILL.md`. Only with `-agent pi` |
| `-timeout` | `120s` | Max time per try |
| `-retries` | `2` | Extra tries after a retryable failure (not used yet) |
| `-host` | `http://192.168.2.156:11434` | Ollama base URL |

Give a suite folder (every `*.yaml` in it runs, in name order) or one or more case files. A case's `fixture` is resolved relative to the case file's folder.

## How it works

```
for each model (and skill set):
  warm-up call (not scored, loads the model into memory)
  for each case, repeated `runs` times:
    send prompt + tools
    model calls list_dir(".") -> harness runs it inside the case fixture -> sends result back
    model writes final answer -> score it
print table + save results/<timestamp>.json
```

`list_dir` can only see files inside the fixture. Paths like `../` or `/etc` are refused.

## Reading the results

```
TARGET           CASE        PASS  ERR  files      latency  tool_called  p50(s)  p95(s)  tok/s
qwen3-coder:30b  list-files  5/5   0    5/5        5/5      5/5          2.1     2.8     54
```

| Column | Meaning |
|---|---|
| TARGET / CASE | The model (or `pi+skills@model`) and the case it ran |
| PASS | Runs where every grader passed |
| ERR | Runs that ended in an error |
| files / latency / tool_called | Runs that passed that grader. Partial scores show their average in brackets |
| p50 / p95 | Median and 95th-percentile time per run, in seconds |
| tok/s | Generation speed of the final answer, as reported by Ollama |

Rows are sorted by PASS, then by p50. Each run's full answer and tool calls are in `results/<timestamp>.json`. Read the failing answers there to check the scoring is fair.

## Changing the test

- **Different folder:** add or remove files in `evals/list-files/fixture/`. The expected answer is read from disk on every run.
- **Different prompt:** edit `prompt` and `system` in `evals/list-files/list-files.yaml`.

## Troubleshooting

**`dial tcp ...: connect: no route to host`, but `curl` to the same host works.**
macOS Local Network privacy is blocking the Go program. Run it from your own terminal (Terminal.app or iTerm), or allow that app under *System Settings → Privacy & Security → Local Network*.

**`... does not support tools`**: that model can't do tool calls. Leave it out with `-models`.
