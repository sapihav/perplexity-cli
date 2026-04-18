# perplexity-cli

Thin CLI wrapper for the Perplexity API (Sonar + Deep Research) — `perplexity` binary. AI-answer search with citations, agent-friendly JSON output.

## Sources of truth

- `../CLI-tools-ROADMAP.md` §5.1 — full spec: commands, output envelope, rate limits, env vars
- `../CLI-tools-ROADMAP.md` §3 — shared conventions across all CLIs
- `../CLI-tools-ROADMAP.md` §4 — security invariants (env-only auth, no shell injection, secret redaction)
- `docs/backlog/_index.md` — current work queue. **Read before starting any task.**

## Status

Phase 1 — foundation CLI per ROADMAP §7. **M1 shipped** — `search` subcommand. Stack: **Go 1.25.6** + `spf13/cobra` + stdlib `net/http` (overrides ROADMAP §2's Python stack).

## Auth

`PERPLEXITY_API_KEY` env var only. No file fallback. If absent: exit code 2 with link to provider's key page.

## Output contract

- **stdout**: always valid JSON, one object per invocation. Common envelope:
  ```json
  {
    "schema_version": "1",
    "provider": "perplexity",
    "command": "sonar",
    "query": "...",
    "result": { "answer": "...", "model": "sonar" },
    "citations": [{ "url": "...", "title": "..." }],
    "cost_usd": 0.0012,
    "elapsed_ms": 410
  }
  ```
- **stderr**: human-readable progress + errors. With `--json-errors`: structured `{error:{message,code,hint?,docs_url?}}`
- **Exit codes**: `0` success / `1` user/config error / `2` API error / `3` network error
- **`--pretty`**: indent JSON for humans

## Standard flags (every subcommand)

`--out <file>`, `--pretty`, `--quiet`, `--verbose`, `--rate-limit N/s`, `--max-retries N`, `--timeout SEC`, `--dry-run`, `--user-agent`, `--json-errors`. Stdin: `-` accepted wherever a query/URL is.

## Self-describing

`perplexity schema` returns the full command tree as JSON (commands, options, output shapes). Agents prefer this over scraping `--help`.

## Conventions

- **Commits**: conventional — `feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`, `perf:`, `ci:`. Breaking change: `type(scope)!:`.
- **Branches**: `feat/<slug>`, `fix/<slug>`, `chore/<slug>`, `docs/<slug>` matching the backlog item's filename.
- **One milestone = one PR**, ≤500 LoC source code (see `.claude/rules/incremental-delivery.md`). Refuse to batch milestones; push back once, comply only if user insists.
- **No shell injection by construction.** Build payloads via the language's JSON serializer, never string-interpolate user input into commands, JSON bodies, or URLs (ROADMAP §4.1).
- **No telemetry, no auto-update, no config files** (ROADMAP §9).

## Backlog workflow

`.claude/rules/backlog.md` is the canonical rule. Quick form:
- Check `docs/backlog/_index.md` before starting
- Set frontmatter `status: in-progress` when you begin
- Set `status: done` and remove from `_index.md` when finished

Use `/feature-create`, `/feature-implement`, `/bug-fix`, `/bug-report` (global skills) — they already write to `docs/backlog/`.

## Subagent delegation

Selective, not default — this codebase is small enough to fit in main context.

- **Explore** — open-ended "where/how" searches across the repo
- **code-reviewer** — mandatory before opening a PR for a milestone
- **senior-backend-engineer** — multi-file milestones (≥3 files: client + command + tests + schema)
- **system-architect** — when designing a new command surface or breaking output change
- **Skip subagents for**: typo fixes, one-line changes, single-file edits — main agent is faster

## Testing expectations

Per ROADMAP §8: mocked HTTP per command, golden-response replay, error-path tests (401/429/500/timeout), `--dry-run` assertions including secret redaction. Coverage ≥ 80%, 100% on retry/ratelimit logic. No real-API hits in CI (gate behind `INTEGRATION=1` env).
