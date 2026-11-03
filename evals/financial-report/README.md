# financial-report

Tests whether a Pi agent can read company financial reports (PDFs) and explain how a number changed from one year to the next, and whether giving it a skill makes the answer better.

## The four questions

Each question is one case file. The prompts are the same except for the metric.

1. `revenue.yaml`: How has revenue changed year over year?
2. `operating-margin.yaml`: How has operating margin changed year over year?
3. `net-profit.yaml`: How has net profit changed year over year?
4. `debt.yaml`: How has debt changed year over year?

Each prompt asks for the number for every year and the change between years. Grading is latency only for now. Read the answers in `results/<timestamp>.json` to judge them. Each run's answer, and any report files Pi wrote, are also saved in `outputs/<timestamp>/<target>/<case>/run-<n>/` here (`answer.md` and `work/`).

## PDFs

Put the financial report PDFs in `pdfs/`. They are not committed (only `pdfs/.gitkeep` is). A case refuses to load while `pdfs/` has no files besides dotfiles. Pi runs inside this folder, so it sees the PDFs as files in the current directory.

## Skills

Each skill is a folder under `skills/` with a `SKILL.md`:

```
skills/
  <name>/SKILL.md
```

You pass the folder name, for example `-skills fin-report`. The run stops before starting if `skills/<name>/SKILL.md` is missing.

## Baseline vs. with a skill

Pass `-skills none` for the baseline (Pi has no skills at all) and `-skills <name>` for a run that is offered the skill. Repeat the flag to compare them in one run:

```bash
go run ./cmd/aieval -agent pi \
  -models ollama/gpt-oss:20b,openrouter/deepseek/deepseek-v4-flash \
  -skills none -skills <name> \
  evals/financial-report
```

This runs every model with every skill set. A run that was offered a skill and did not load it is retried (2 times by default), then fails with `skill not loaded: <name>`. Raise `runs:` in a case file to repeat a question.

macOS Local Network permission blocks apps like the Claude app from reaching the Ollama box; run it from your own terminal.
