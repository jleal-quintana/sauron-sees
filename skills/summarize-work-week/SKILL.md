---
name: summarize-work-week
description: Summarize daily work markdowns into a weekly focus update and manager-ready status note. Use when Codex needs to read one week of `YYYY-MM-DD-work-summary.md` files from an Obsidian vault or similar folder and produce: a weekly focus summary, recurrent themes, meeting highlights, delivered work, open threads, or a concise manager update.
---

# Summarize Work Week

Summarize a week of daily work summaries without reprocessing screenshots or raw artifacts.
Read only the daily markdown files, aggregate them, and produce a concise weekly report.

## Workflow

1. Generate or refresh the weekly summary.
Use the Go CLI to resolve the target week and generate the weekly markdown from daily summaries.

Examples:

```bash
sauron-sees weekly-summary --week 2026-W10
sauron-sees weekly-summary --from 2026-03-02 --to 2026-03-06
```

2. Read the generated weekly markdown.
The Go command writes a validated weekly markdown into the configured weekly folder.

3. Refine only if requested.
If the user wants a shorter email or a different tone, edit the generated weekly markdown while preserving the factual content and structure in `references/output-format.md`.

## Rules

- Do not read screenshots, manifests, or temp folders.
- Do not call Granola in this skill. Use only the daily markdowns and the generated weekly markdown.
- Prefer cross-day patterns over repeating each day verbatim.
- Separate concrete outcomes from possible inferences.
- If the user asks for a mail-ready version, compress the `Manager Update Draft` section into 5-8 sentences.

## Inputs

- Use either `--week YYYY-Www` or both `--from YYYY-MM-DD` and `--to YYYY-MM-DD`.
- The CLI reads files matching `YYYY-MM-DD-work-summary.md` from the configured daily folder and writes a weekly markdown to the configured weekly folder.

## Outputs

- Weekly focus summary.
- Recurring projects or themes.
- Meetings and decisions worth surfacing upward.
- Concrete deliverables or visible progress.
- Open threads and next steps.
- Manager update draft.

## References

- Read `references/output-format.md` before drafting the final answer.
