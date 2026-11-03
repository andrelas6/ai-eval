# ai-eval

An eval for local Ollama models, and for the Pi coding agent with different models and skills. The built-in `list-files` suite gives each model the prompt **"list the files of this directory"** and a `list_dir` tool. Each answer is scored on:

- **Content**: did it name the real files and avoid made-up ones?
- **Speed**: how long did it take, from the first request to the final answer?

## Requirements

- Go 1.26+
- An Ollama server you can reach (default `http://192.168.2.156:11434`) with models that support tool calling
- For `-agent pi`: the `pi` command on your PATH, on macOS or Linux

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
| `-agent` | `ollama` | `ollama` or `pi` |
| `-models` | *(required)* | Comma-separated model names. With `-agent pi` write `provider/model` |
| `-skills` | *(none)* | A skill set: `none`, or names joined with commas. Repeat the flag for more sets. Names map to `<suite>/skills/<name>/SKILL.md`. Only with `-agent pi` |
| `-timeout` | `120s` | Max time per try |
| `-retries` | `2` | Extra tries after a retryable failure (a failed skill check is one) |
| `-host` | `http://192.168.2.156:11434` | Ollama base URL |

Give a suite folder (every `*.yaml` in it runs, in name order) or one or more case files. A case's `fixture` is resolved relative to the case file's folder.

## How it works

### What happens when you run the command

```bash
go run ./cmd/aieval -agent pi -models ollama/gpt-oss:20b -skills none -skills fin evals/financial-report
```

```text
cmd/aieval/main.go  run()
 │
 ├─1 check flags      -models required · pi models must be provider/model
 │                    -skills only with pi · -retries ≥ 0
 │
 ├─2 caseFiles()      evals/financial-report → its *.yaml, sorted, no duplicates
 │
 ├─3 skillDirs()      "none" → []          (baseline)
 │                    "fin"  → [/abs/real/path/evals/financial-report/skills/fin]
 │                    checks SKILL.md exists, plain names only, symlinks resolved
 │
 ├─4 eval.LoadCase()  per yaml: fixture "pdfs" → evals/financial-report/pdfs
 │                    (fails if missing or empty)
 │
 ├─5 targets = models × skill sets
 │      pi@ollama/gpt-oss:20b         (no skills)
 │      pi+fin@ollama/gpt-oss:20b     (skills: [fin])
 │
 ├─6 eval.Runner.Run(targets, cases)      ◀ the runner loop, below
 │
 └─7 report.Summarize → WriteTable (terminal)
     report.WriteJSON → results/<timestamp>.json
```

### The runner loop

```text
for each target                      (pi@…, pi+fin@…)
  warm-up?  only if the target has WarmUp()      → Ollama yes, Pi no
  for each case                      (revenue, operating-margin, net-profit, debt)
    for n in 1..runs
      runOnce:
        try 1 ──▶ tryOnce (own -timeout) ──▶ t.Run(Input{Prompt, System, Dir: fixture})
          │
          ├─ ok             → run graders → Result{Pass, Scores}            done
          ├─ RetryError     → log "try 1 failed, retrying: skill not loaded: fin"
          │                   try 2, try 3 (with -retries 2)
          │                   still failing → Result{Err: "skill not loaded: fin"}
          └─ other error    → Result{Err}, no retry
        cost and tokens are added up over every try
        Result.Tries = how many tries it took
```

The runner doesn't know about skills. A `RetryError` from a target just means "try again". The Pi target returns one when an offered skill's `SKILL.md` was never read.

With `-agent ollama`, the harness runs the case's tools itself. `list_dir` can only see files inside the fixture, and paths like `../` or `/etc` are refused.

## Pi agent

`-agent pi` runs each case with the `pi` command in the case's fixture folder. A target is a model plus a skill set, named `pi@<model>` or `pi+<skills>@<model>`.

```bash
go run ./cmd/aieval -agent pi -models ollama/gpt-oss:20b -skills none evals/financial-report
```

Once `skills/<name>/SKILL.md` exists in the suite, add `-skills <name>` to compare it with the baseline.

- **Models** are written `provider/model`, split at the first `/`: `ollama/gpt-oss:20b` runs `--provider ollama --model gpt-oss:20b`, and `openrouter/deepseek/deepseek-v4-flash` runs `--provider openrouter --model deepseek/deepseek-v4-flash`. The `ollama` provider comes from Pi's own `~/.pi/agent/models.json`, not from `-host`.
- **Pinned flags.** Only the model and the skills change between targets, so a difference in results comes from them. Every run passes `-p --mode json --no-session --no-skills --no-context-files --no-extensions --no-prompt-templates --no-approve --tools read,bash --thinking medium`. In plain words: one print-mode run with JSON events, no saved session, no skills, context files, extensions or prompt templates picked up from the machine, no approval prompts, only the `read` and `bash` tools, and the same thinking level. A case's `system` text is added with `--append-system-prompt`.
- **Skills: offered vs. loaded.** Offering a skill (`--skill <dir>`) only lists its name and description to the model. The model decides whether to read `SKILL.md`. A skill counts as loaded when the model read its `SKILL.md` with `read`, or named it in a `bash` command, and the tool call worked. A baseline (`-skills none`) offers nothing and is never retried.
- **Retries.** If a skill was offered and not loaded, the try is repeated, up to `-retries` times (2 by default). After that the run is an error: `skill not loaded: <name>`. Each try gets a fresh `-timeout`.
- **Unix only.** Running `pi` needs process groups, so it works on macOS and Linux only. `pi` is stopped, with anything it started, when a try times out.

## Reading the results

```
TARGET                                               CASE     PASS  ERR  latency  p50(s)  p95(s)  tok/s  RETRIES  COST      SKILLS
pi@ollama/gpt-oss:20b                                revenue  1/1   0    1/1      40.0    40.0    -      -        -         -
pi+fin-report@ollama/gpt-oss:20b                     revenue  1/1   0    1/1      40.0    40.0    -      1        -         fin-report 1/1
pi@openrouter/deepseek/deepseek-v4-flash             revenue  1/1   0    1/1      40.0    40.0    -      -        <$0.0001  -
pi+fin-report@openrouter/deepseek/deepseek-v4-flash  revenue  1/1   0    1/1      40.0    40.0    -      -        $0.0031   fin-report 1/1
```

Ollama runs on `list-files` look like this, with more grader columns:

```
TARGET           CASE        PASS  ERR  files  latency  tool_called  p50(s)  p95(s)  tok/s  RETRIES  COST  SKILLS
qwen3-coder:30b  list-files  5/5   0    5/5    5/5      5/5          2.1     2.8     54     -        -     -
```

| Column | Meaning |
|---|---|
| TARGET / CASE | The model (or `pi+skills@model`) and the case it ran |
| PASS | Runs where every grader passed |
| ERR | Runs that ended in an error |
| grader columns (`files`, `latency`, `tool_called`) | Runs that passed that grader. Partial scores show their average in brackets. `-` means the case doesn't use that grader |
| p50 / p95 | Median and 95th-percentile time per run, in seconds |
| tok/s | Generation speed of the final answer, as reported by Ollama. `-` when no run reports it (Pi doesn't) |
| RETRIES | Extra tries across the runs of the row. `-` means none |
| COST | Money spent over all tries, as reported by Pi. `<$0.0001` means above zero but less than that. `-` means no cost was reported (Ollama doesn't report one) |
| SKILLS | Each offered skill with the runs that loaded it out of the runs that were offered it, e.g. `fin-report 3/3`. `-` means no skill was offered. Runs that errored before the skill check ran aren't counted |

Rows are sorted by case, then PASS, then p50. Each run's full answer and tool calls are in `results/<timestamp>.json`. Read the failing answers there to check the scoring is fair. In that file, `cost` and `tokens` add up every try of a run, including retried ones, while `turns`, `answer` and the skills lists come from the last try only.

## Changing the test

- **Different folder:** add or remove files in `evals/list-files/fixture/`. The expected answer is read from disk on every run.
- **Different prompt:** edit `prompt` and `system` in `evals/list-files/list-files.yaml`.

## Troubleshooting

**`dial tcp ...: connect: no route to host`, but `curl` to the same host works.**
macOS Local Network privacy is blocking the Go program. Run it from your own terminal (Terminal.app or iTerm), or allow that app under *System Settings → Privacy & Security → Local Network*.

**`... does not support tools`**: that model can't do tool calls. Leave it out with `-models`.
