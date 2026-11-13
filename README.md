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
| `-min-pass` | *(none)* | A pass rate from 1 to 100, e.g. `90%` or `90`. Marks the cheapest grid cell that reaches it (see Reading the grid) |
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
        try 1 ──▶ tryOnce (own -timeout) ──▶ t.Run(Input{Prompt, System, Dir: fixture})   ← Pi copies the fixture first
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

`-agent pi` runs each case with the `pi` command in a temporary copy of the case's fixture folder (see Soft sandbox below). A target is a model plus a skill set, named `pi@<model>` or `pi+<skills>@<model>`.

```bash
go run ./cmd/aieval -agent pi -models ollama/gpt-oss:20b -skills none evals/financial-report
```

Once `skills/<name>/SKILL.md` exists in the suite, add `-skills <name>` to compare it with the baseline.

- **Models** are written `provider/model`, split at the first `/`: `ollama/gpt-oss:20b` runs `--provider ollama --model gpt-oss:20b`, and `openrouter/deepseek/deepseek-v4-flash` runs `--provider openrouter --model deepseek/deepseek-v4-flash`. The `ollama` provider comes from Pi's own `~/.pi/agent/models.json`, not from `-host`.
- **Pinned flags.** Only the model and the skills change between targets, so a difference in results comes from them. Every run passes `-p --mode json --no-session --no-skills --no-context-files --no-extensions --no-prompt-templates --no-approve --tools read,bash --thinking medium`. In plain words: one print-mode run with JSON events, no saved session, no skills, context files, extensions or prompt templates picked up from the machine, no approval prompts, only the `read` and `bash` tools, and the same thinking level. A case's `system` text is added with `--append-system-prompt`.
- **Skills: offered vs. loaded.** Offering a skill (`--skill <dir>`) only lists its name and description to the model. The model decides whether to read `SKILL.md`. A skill counts as loaded when the model read its `SKILL.md` with `read`, or named it in a `bash` command, and the tool call worked. A baseline (`-skills none`) offers nothing and is never retried.
- **Retries.** If a skill was offered and not loaded, the try is repeated, up to `-retries` times (2 by default). After that the run is an error: `skill not loaded: <name>`. Each try gets a fresh `-timeout`.
- **Unix only.** Running `pi` needs process groups, so it works on macOS and Linux only. `pi` is stopped, with anything it started, when a try times out.

### Soft sandbox

- **Where.** Each try runs in `$TMPDIR/aieval-*/work`, a copy of the fixture. On macOS that is under `/var/folders/…/T`, and on Linux under `$TMPDIR` or `/tmp`.
- **Skills.** Offered skills are copied to `../skills/<name>`, and `--skill` points there.
- **Cleanup.** The folder is deleted after each try, and the results JSON records its path as `sandbox`.
- **What it blocks.** `..` paths can't reach the case files, skills, README, earlier results or git.
- **What it doesn't block.** Absolute paths such as `cat /Users/...`. That would need an OS sandbox.
- **noexec.** On Linux, a `/tmp` mounted `noexec` stops skill scripts from running as `./x.sh`. `bash x.sh` still works.
- **Small `/tmp`.** Set `TMPDIR` to a bigger folder.

## Saved outputs

Each run's answer is saved next to its case, as soon as the run ends:

```text
<suite>/outputs/<timestamp>/<target>/<case>/run-<n>/
  answer.md     the model's final reply (every agent)
  error.txt     only when the run errored
  work/...      new files the model made in the sandbox (Pi only)
```

- **Final try only.** A retried run saves its last try.
- **New files only.** `work/` holds files the model created. A fixture file it changed is not copied, and symlinks are skipped.
- **Names.** `/` and `:` in the target name become `_`. The timestamp is the same one as the results file name.
- **Git.** `evals/*/outputs/` is ignored.
- **In the results JSON.** Each result has `suite` (the case's folder) and `output_dir` (its run folder, empty if saving failed).
- **Warnings.** If saving fails, or Pi could not read the new files (`files_error` in the trace), a `warning:` line is printed to stderr and the eval carries on.

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

Rows are sorted by case, then PASS, then p50. Each run's full answer and tool calls are in the `results` list of `results/<timestamp>.json`. Read the failing answers there to check the scoring is fair. In that file, `cost` and `tokens` add up every try of a run, including retried ones, while `turns`, `answer` and the skills lists come from the last try only.

## Reading the grid

Under the table, a grid shows every skill set against every model:

```
SKILLS \ MODEL  ollama/gpt-oss:20b           openrouter/deepseek/deepseek-v4-flash
none            1/2    50%   $0     40.0s    2/2  100%  <$0.0001  35.0s
fin-report      * 2/2  100%  $0     38.0s    2/2  100%  $0.0031   30.0s
* cheapest at ≥ 90% pass: fin-report @ ollama/gpt-oss:20b
```

- **Rows and columns.** Rows are skill sets (`none` first, then in flag order). Columns are models. A target with no skills is the `none` row.
- **Cell parts.** Passed runs out of all runs, the pass rate, the cost, and the median time (p50). Runs are added up over every case. `-` in the first part means the pair had no runs.
- **Errors count as fails.** An errored run adds to the runs but not to the passes, and its time is left out of the p50. If every run errored, the p50 is `-`.
- **Rate rounds down.** 2 of 3 prints `66%`, so `100%` always means every run passed.
- **Cost.** `$0` means no cost was reported (Ollama never reports one) or it was zero. `<$0.0001` means above zero but less than that.
- **The pick.** With `-min-pass`, the cells whose real pass rate is at least that value qualify (a cell exactly at it qualifies, the rounded percent is not used). The cheapest one wins, so a `$0` cell beats a paid one. On equal cost the lower p50 wins. The cell gets a `*` and the line under the grid names it. If no cell qualifies the line says `no cell reached 90% pass`. Without the flag there is no `*` and no line.

The results JSON is an object `{results, grid}`. `results` is the list of runs. `grid` has `rows`, `columns`, `cells` (`runs`, `pass`, `cost`, `p50_ns`) and, only with `-min-pass`, `min_pass` and `pick` (`skills`, `model`, left out when no cell qualifies).

## Graders

A case lists its graders under `graders:`. Each one is a name, or `name: settings`. Every grader gives a score with a `Value` (0 to 1), a `Pass` (true or false) and a `Note` that says why it failed. A run passes when every grader passes. A bad setting is refused when the case loads, with the grader name in the error, before any model is called.

```yaml
graders:
  - latency: { max: 90s }
  - not_contains: ["unable to read", "can't access", "cannot access"]
  - contains: ["2025", "2024"]
  - regex: '\|\s*2025\s*\|'
  - number: { values: [416161 million, 391035 million], within: 1% }
  - file_exists: report.md
  - file_contains: { file: report.md, text: ["revenue"] }
```

**List each grader name once per case.** Listing the same name twice is refused: `grader "contains" is listed twice; list all the values in one contains`. The table and the grid show one column per grader name and count each run once per score, so two `number` graders would count twice per run. Put several values in one grader instead.

| Grader | Passes when | Settings |
|---|---|---|
| `files` | The answer names every top-level entry of the fixture and no made-up filenames. `Value` is the share named | none. The case needs a `fixture` |
| `tool_called` | The model called that tool at least once | the tool name, e.g. `tool_called: list_dir`. The case must offer it in `tools` |
| `latency` | The run took no longer than `max`. With no `max` it always passes and only records the time | `max`, a duration like `10s` or `1m30s` |
| `contains` | The answer holds every text, ignoring case | one text or a list |
| `not_contains` | The answer holds none of the texts, ignoring case | one text or a list |
| `regex` | The pattern matches anywhere in the answer | one pattern |
| `number` | Every expected figure appears in the answer within a tolerance | `values` (a list) and `within` |
| `file_exists` | A file the run created matches the pattern | one pattern |
| `file_contains` | One matching file holds every text, ignoring case | `file` (a pattern) and `text` (one text or a list) |

For the list graders, `Value` is the share of items that matched (for `not_contains`, the share that were absent), and `Pass` needs all of them. The `Note` names what is missing (`missing: 2025`) or found (`found: unable to read`).

### contains and not_contains

Plain text, case ignored. A single text can be written without the brackets: `not_contains: "can't access"`. Empty or blank items are refused. Use `not_contains` to catch refusals, so an answer like "I was unable to read the PDFs" stops passing.

### regex

The pattern is compiled when the case loads, so a typo fails before any run. It uses Go's RE2 syntax. It is case-sensitive unless the pattern starts with `(?i)`.

**Write the pattern in single quotes.** Single quotes keep backslashes as written, so `'\|\s*2025\s*\|'` is the pattern `\|\s*2025\s*\|`. Double quotes drop the backslash before most characters (`\n` and `\t` become a newline and a tab), so `"\|\s*2025"` silently becomes `|s*2025`.

### number

`values` lists the figures the answer must contain, written the way you would write them. Each item must hold exactly one number. `within` is a relative tolerance in percent, written with `%` (default `1%`): a figure matches when `|answer - expected| <= within * |expected|`.

The reader understands these forms, and an expected value and the answer are read the same way:

- Plain numbers: `416161`, `416,161`, `1,234.5`, `-12`, years like `2025`.
- Scales, with or without a space: `$416.2 billion`, `416,161 million`, `$416.2B`, `416.2bn`, `12 thousand`, `3k`, `1.5 trillion`. The scale is applied, so `416161 million` and `$416.2 billion` are the same figure. Scale words are `thousand`, `million`, `billion`, `trillion`, and the short forms `k`, `m`, `mn`, `b`, `bn`.
- Percents: `31.97%`, `-3.5%`, `31.97 %`. A minus sign or a Unicode minus both work.

Things to know:

- **One-letter scales must touch the number.** `5m` is 5 million but `5 m` is 5, and `4 M&A deals` is 4. Longer scales can have a space.
- **Words are kept whole.** `5 more` and `10 months` are 5 and 10, not millions. Digits glued to other letters (`10x`, `3rd`, `Q3`, `report2025.pdf`) are not read as numbers.
- **A percent only matches a percent.** `31.97%` matches `32.0%` but not `31.97` written without a percent sign, and a plain figure never matches a percent.
- **Bare table figures read as plain numbers.** A table in millions that shows `416,161` reads as 416161, not 416161 million, and fails. Ask for units in the prompt, for example "write every amount with its unit, like $12.3 billion".
- **A wide `within` can let one answer number satisfy two expected values.** With `within: 10%`, a single `95` in the answer matches both `90` and `100`. Keep the tolerance small when expected values are close together.
- **An expected `0` only matches exactly**, because the tolerance is a share of the expected value.
- **Matches count anywhere in the answer**, so a year can match a figure: an expected plain `2030` (within 1%) is matched by the year `2025` in the text. Keep expected values distinctive.

`Value` is the share of expected figures found. The note reads `no number within 1% of: 391035 million, 383285 million`, listing each missing figure as you wrote it. `within` must be above 0 and finite.

### file_exists and file_contains

These look at files the run created in its sandbox (see Soft sandbox), which are the same files saved under `work/`.

- **Patterns** use Go's `path.Match`. `report.md` matches that exact name, and `*.md` matches any markdown file at the top level of the sandbox. `*` does not cross a folder, so `*.md` does not match `sub/x.md`; write `sub/*.md` for that. A bad pattern is refused when the case loads.
- **Only new files count.** A fixture file the model edited is not seen, and neither are symlinks.
- **Text matching is meant for text files.** `file_contains` compares lowercased file bytes with the texts, so binary files such as PDFs won't give useful matches.
- **`file_contains` needs one file to hold everything.** With `*.md`, the texts can't be spread over two files. `Value` is the best single file's share of the texts. The note names what each matching file lacks, e.g. `a.md missing: 2025; b.md missing: Revenue`.
- **No match.** The note lists up to five of the files that were written, sorted, e.g. `no file matches report.md; wrote: a.txt, b.txt, c.txt, d.txt, e.txt and 2 more`.
- **Ollama writes no files.** Ollama targets only answer, so these graders fail with `the target wrote no files`. The case still loads. Use them with `-agent pi`.
- **Unreadable files.** If Pi could not read the new files (the `files_error` warning), a grader that finds no match says `could not read the target's files: <error>` instead of blaming the model.

## Changing the test

- **Different folder:** add or remove files in `evals/list-files/fixture/`. The expected answer is read from disk on every run.
- **Different prompt:** edit `prompt` and `system` in `evals/list-files/list-files.yaml`.

## Troubleshooting

**`dial tcp ...: connect: no route to host`, but `curl` to the same host works.**
macOS Local Network privacy is blocking the Go program. Run it from your own terminal (Terminal.app or iTerm), or allow that app under *System Settings → Privacy & Security → Local Network*.

**`... does not support tools`**: that model can't do tool calls. Leave it out with `-models`.
