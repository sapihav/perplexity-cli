---
title: M3 — standalone `/search` endpoint
type: task
priority: P1
status: todo
created: 2026-04-18
---

# M3 — standalone `/search` endpoint

## Problem Statement

MCP `perplexity_search` hits `POST /search` (not `/chat/completions`) and returns ranked, un-synthesized web results. Our current `search` subcommand uses chat completions and returns an AI answer. Agents that want plain search results cannot get them via this CLI today.

## Acceptance Criteria

- Rename current `search` semantic: keep `perplexity search "<query>"` but migrate it to hit `POST /search` (not chat completions).
  - **Breaking change** for existing users of `search`. Communicate via CHANGELOG entry.
  - AI-answered behaviour remains available via `perplexity ask` (M2).
- New flags on `search`:
  - `--max-results N` (1–20, default 10)
  - `--country ISO`
  - `--language ISO`
  - `--domain DOMAIN` (repeatable) / `--exclude-domain DOMAIN`
  - `--recency DURATION` (e.g. `1d`, `1w`, `1m`)
  - `--date-from YYYY-MM-DD`, `--date-to YYYY-MM-DD`
- Output envelope: `result.results[].{title, url, snippet, published_date, domain}`; no `answer` field for this command.
- All M2 contract flags (`--dry-run`, `--timeout`, `--json-errors`) work.

## Context / Notes

- Endpoint reference: https://docs.perplexity.ai/api-reference/search-post
- Depends on M2 (contract flags must exist first).
- Budget: ~350 LoC (new request/response models, migrate `cmd/search.go`, tests + golden response).
