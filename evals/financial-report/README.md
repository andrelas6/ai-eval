# financial-report

Tests whether a Pi agent can read company financial reports (PDFs) and explain how a number changed from one year to the next, and whether giving it a skill makes the answer better.

## The four questions

Each question is one case file. The prompts are the same except for the metric.

1. `revenue.yaml`: How has revenue changed year over year?
2. `operating-margin.yaml`: How has operating margin changed year over year?
3. `net-profit.yaml`: How has net profit changed year over year?
4. `debt.yaml`: How has total term debt changed year over year?

Each prompt asks for the number for every year and the change between years. The revenue, net profit and debt prompts also ask for every amount with its unit, so a bare table figure doesn't count. The operating margin prompt doesn't ask for units, so the answer has to write margins with `%` (like `31.97%`): a bare `31.97` or `0.3197` won't match. Debt means total term debt.

## What is graded

Each case has three graders, so a pass means the right figures and not only an answer in time:

- `latency`: records how long the run took.
- `not_contains`: the answer must not say "unable to read", "can't access" or "cannot access".
- `number`: the answer must hold each figure below within 1%, in any common form (`$416.2 billion`, `416,161 million`).

The figures come from Apple's FY2025 annual report (income statement for fiscal 2025, 2024 and 2023; balance sheet for 2025 and 2024), in US$ millions:

| Case | Figures |
|---|---|
| `revenue.yaml` | 416161, 391035, 383285 |
| `net-profit.yaml` | 112010, 93736, 96995 |
| `operating-margin.yaml` | 31.97%, 31.51%, 29.82% |
| `debt.yaml` (total term debt) | 90678, 96662 |

The debt case checks two years, and the others check three. See Graders in the main README for how the checks read numbers. Read the answers in `results/<timestamp>.json` to judge anything the checks can't, such as the change between years. Each run's answer, and any report files Pi wrote, are also saved in `outputs/<timestamp>/<target>/<case>/run-<n>/` here (`answer.md` and `work/`).

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
