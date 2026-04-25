# Changelog

All notable changes to `perplexity-cli` are recorded here. Format loosely
follows [Keep a Changelog](https://keepachangelog.com); the project uses
[Semantic Versioning](https://semver.org).

## v0.5.0 — 2026-04-25

### Added

- `perplexity reason <query>` — chain-of-thought subcommand backed by
  `sonar-reasoning-pro` (closes parity with the official MCP
  `perplexity_reason` tool). Same flag set as `ask` (`--system`,
  `--messages @file.json`, `--max-tokens`) plus `--strip-thinking`
  (default `true`) which extracts `<think>...</think>` /
  `<thinking>...</thinking>` blocks from the response and surfaces them
  under `result.thinking`. Pass `--strip-thinking=false` to keep the raw
  content in `result.answer`. `--model` defaults to `sonar-reasoning-pro`
  and accepts `sonar-reasoning` for the smaller variant.
- Internal: `runChatCompletion` shared helper now backs both `ask` and
  `reason` so the chat-completions code path is parameterized over model
  with a single implementation.

### Output payload

- `reason` → `{answer, thinking?, model, citations[]}` under the standard
  envelope. `thinking` is omitted when `--strip-thinking=false` or when
  the upstream response carries no `<think>` block.

## v0.4.0 — 2026-04-19

### Breaking changes

- `perplexity search` now targets `POST /search` instead of
  `POST /chat/completions`. The command returns ranked web results with no
  AI synthesis. The `result` payload changes from
  `{answer, model, citations[]}` to `{results[]{title,url,snippet,published_date,domain}}`.
  The top-level envelope (`schema_version`, `provider`, `command`,
  `elapsed_ms`) is unchanged.
- The `--model` flag has been removed from `search` (there is no model
  choice on `/search`). `ask` keeps its own `--model`, unchanged.

### Migration

If you rely on AI-synthesized answers, migrate to `perplexity ask`:

```sh
# before (v0.3.x)
perplexity search "summarise RAFT"

# after (v0.4.0)
perplexity ask "summarise RAFT"
```

If you want ranked web results, `perplexity search` is still the right
command — just expect `result.results[]` instead of `result.answer`.

### Added

- `perplexity search` flags: `--max-results` (1–20, default 10),
  `--country`, `--language`, `--domain` / `--exclude-domain` (repeatable),
  `--recency` (`1h|1d|1w|1m|1y`), `--date-from` / `--date-to`
  (`YYYY-MM-DD`).
- Each result carries a derived `domain` field (from the URL's hostname,
  with a leading `www.` stripped) for grouping without client-side parsing.

### Unchanged

- `ask`, `schema`, `version` subcommands.
- All workspace contract flags on the root command: `--dry-run`,
  `--timeout`, `--json-errors`, `--rate-limit`, `--user-agent`,
  `--pretty`, `--out`, `--verbose`, `--quiet`, stdin `-`.
- Exit-code taxonomy: `0` success, `1` API error, `2` user/config error,
  `3` network error.
