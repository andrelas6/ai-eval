# ai-eval

A small eval for local Ollama models. Each model gets the prompt **"list the files of this directory"** and a `list_dir` tool. Each answer is scored on:

- **Content**: did it name the real files and avoid made-up ones?
- **Speed**: how long did it take, from the first request to the final answer?

## Requirements

- Go 1.26+
- An Ollama server you can reach (default `http://192.168.2.156:11434`) with models that support tool calling

## Run

Quick check with one model and one run:

```bash
go run . -models qwen3.5:9b -runs 1
```

Full eval, with every model on the server and 5 runs each:

```bash
go run .
```

Unit tests (scoring and the tool):

```bash
go test ./...
```

### Flags

| Flag | Default | What it does |
|---|---|---|
| `-host` | `http://192.168.2.156:11434` | Ollama base URL |
| `-models` | *(all)* | Comma-separated model names. Empty means every model from `/api/tags` |
| `-runs` | `5` | Scored runs per model |
| `-dir` | `testdata/sample` | Folder the model is asked to list |
| `-timeout` | `120s` | Max time per run |

Example:

```bash
go run . -models gpt-oss:20b,qwen3-coder:30b -runs 3
```

## How it works

```
for each model:
  warm-up call (not scored, loads the model into memory)
  repeat N times:
    send prompt + list_dir tool
    model calls list_dir(".") → harness runs it inside testdata/sample → sends result back
    model writes final answer → score it
print table + save results/<timestamp>.json
```

`list_dir` can only see files inside `-dir`. Paths like `../` or `/etc` are refused.

## Reading the results

```
MODEL            PASS  RECALL  HALLUC  TOOL  p50(s)  p95(s)  tok/s
qwen3-coder:30b  5/5   1.00    0.0     5/5   2.1     2.8     54
```

| Column | Meaning |
|---|---|
| PASS | Runs where every real entry was named and no file was made up |
| RECALL | Average share of the top-level entries named in the answer (1.00 = all) |
| HALLUC | Average count of filenames in the answer that don't exist in the folder |
| TOOL | Runs where the model actually called `list_dir` |
| p50 / p95 | Median and 95th-percentile time per run, in seconds |
| tok/s | Generation speed of the final answer, as reported by Ollama |

Rows are sorted by PASS, then by p50. Each run's full answer and tool calls are in `results/<timestamp>.json`. Read the failing answers there to check the scoring is fair.

## Changing the test

- **Different folder:** add or remove files in `testdata/sample/`. The expected answer is read from disk on every run.
- **Different prompt:** edit `systemPrompt` and `userPrompt` in `main.go`.

## Troubleshooting

**`dial tcp ...: connect: no route to host`, but `curl` to the same host works.**
macOS Local Network privacy is blocking the Go program. Run it from your own terminal (Terminal.app or iTerm), or allow that app under *System Settings → Privacy & Security → Local Network*.

**`... does not support tools`**: that model can't do tool calls. Leave it out with `-models`.
