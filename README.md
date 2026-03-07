# Sauron Sees

Windows-first background agent in Go that captures periodic screenshots, generates daily and weekly work summaries with Codex, and only deletes temporary artifacts after a second verification pass says the output is safe.

## What It Does

- Runs in the background on Windows.
- Captures screenshots every `X` minutes during configured work hours.
- Builds a daily work summary markdown in Obsidian-ready format.
- Builds a weekly work summary markdown with a manager-ready email draft.
- Uses a second Codex pass plus local file checks before deleting temporary screenshots.
- Optionally uses Granola MCP during the daily summary flow.

## Main Commands

```bash
sauron-sees agent
sauron-sees capture-now
sauron-sees close-day [--date YYYY-MM-DD]
sauron-sees weekly-summary [--week YYYY-Www]
sauron-sees weekly-summary --from YYYY-MM-DD --to YYYY-MM-DD
sauron-sees doctor
sauron-sees install-startup
sauron-sees uninstall-startup
```

## Configuration

Copy `config.example.toml` into your user config directory and adjust:

- temporary capture root
- daily markdown root
- weekly markdown root
- capture cadence and work hours
- daily and weekly minimum word counts
- weekly auto-close day and time
- Codex profile and Granola MCP settings

## Current Status

- `go test ./...` passes
- `GOOS=windows GOARCH=amd64 go build ./...` passes
- Real runtime behavior still needs end-to-end validation on Windows with Codex and Granola configured locally
- SSH push is configured to use the `github-work` host alias for this repository
- Daily and weekly summaries are designed to be copied directly into Obsidian and manager updates

## License

MIT
