---
title: M2 — `ask` subcommand + contract flag hardening
type: task
priority: P1
status: todo
created: 2026-04-18
---

# M2 — `ask` subcommand + contract flag hardening

## Problem Statement

Today `perplexity search` is the only subcommand. The official Perplexity MCP exposes `perplexity_ask` (conversational, backed by `sonar-pro`) as a distinct tool with different default model and use case. We also lack most of the workspace-standard flags (`--dry-run`, `--timeout`, `--json-errors`, `--rate-limit`, stdin `-`, and `schema`), blocking pipeline/CI use.

## Acceptance Criteria

- `perplexity ask "<query>"` subcommand that calls `/chat/completions` with `sonar-pro` by default.
  - Flags: `--model` (sonar / sonar-pro — default `sonar-pro`), `--max-tokens`, `--system <prompt>`, `--messages @file.json` for multi-turn.
  - Reads `-` from stdin for the query.
- Global flags added and plumbed through every existing and new command:
  - `--dry-run` — prints HTTP method + URL + headers (API key redacted) + JSON body, exits 0, no network call.
  - `--timeout SEC` — per-request timeout; default 60s for `ask`/`search`.
  - `--json-errors` — switches stderr error output to `{error:{message,code,hint?,docs_url?}}`.
  - `--rate-limit N/s` — client-side semaphore, default 1/s.
  - `--user-agent` + `PERPLEXITY_USER_AGENT` env var override.
- `perplexity schema` subcommand — emits the full command tree (commands, flags, output shapes) as JSON.
- Redact `Authorization` header in `--verbose` and `--dry-run` output.

## Context / Notes

- Wraps `POST /chat/completions`. Keep `ask` and `search` as two thin command files that share one request builder in `internal/client/`.
- `ask` is the MCP `perplexity_ask` equivalent; do not remove or rename `search`.
- Budget: ~450 LoC (new `cmd/ask.go`, shared flag plumbing in `cmd/root.go`, new `cmd/schema.go`, client extensions, tests).
- Reference: MCP tool names and default models documented at https://docs.perplexity.ai/guides/mcp-server.
