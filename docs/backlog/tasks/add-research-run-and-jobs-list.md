---
title: M6 — `research run` (blocking) + `jobs list`
type: task
priority: P2
status: todo
created: 2026-04-18
---

# M6 — `research run` (blocking) + `jobs list`

## Problem Statement

Agents often want the research result synchronously without writing a shell loop. Also, when multiple research jobs are in flight, there's no way to enumerate them — the MCP doesn't expose this surface at all.

## Acceptance Criteria

- `perplexity research run "<prompt>"` — submit + poll with exponential backoff until terminal state, print final envelope.
  - `--poll-interval SEC` (default 10s, max 60s).
  - `--max-wait SEC` (default 1800s / 30min). On timeout, exit 3 with last-known status.
  - Supports same flags as `research submit`.
- `perplexity jobs list` — GET `/async/chat/completions` (list endpoint). Returns `result.jobs[]` with job_id, status, created_at, model.
  - `--status CREATED|IN_PROGRESS|COMPLETED|FAILED` filter (client-side).
  - `--limit N`.

## Context / Notes

- Depends on M5 (uses same client methods).
- Budget: ~300 LoC.
- `jobs list` is a CLI-native advantage over the MCP (MCP hides job IDs entirely).
- Community forum note: list vs get status fields may differ — test both. See https://community.perplexity.ai/t/async-endpoints-return-different-status-for-list-vs-get-on-completions-endpoint/3681
