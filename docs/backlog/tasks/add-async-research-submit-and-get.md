---
title: M5 — `research submit` + `research get` (async core)
type: task
priority: P1
status: todo
created: 2026-04-18
---

# M5 — `research submit` + `research get` (async core)

## Problem Statement

MCP `perplexity_research` hides the async job lifecycle. Perplexity's `/async/chat/completions` requires submit-then-poll for `sonar-deep-research` because runs routinely exceed HTTP timeouts. The CLI needs explicit submit and poll commands so pipelines can fire-and-forget plus pick up results later.

## Acceptance Criteria

- `perplexity research submit "<prompt>"` — POST `/async/chat/completions` with `model=sonar-deep-research`.
  - Flags: `--reasoning-effort low|medium|high` (default `medium`), `--system`, `--messages @file.json`.
  - Output envelope: `result.{job_id, status, model, created_at}`. Exits immediately.
- `perplexity research get <job_id>` — GET `/async/chat/completions/{id}`.
  - Output envelope: `result.{job_id, status, answer?, reasoning?, model, created_at, completed_at?}` + `citations[]`.
  - `status` values: `CREATED`, `IN_PROGRESS`, `COMPLETED`, `FAILED`.
- Exit codes: `0` on `COMPLETED`, `1` on `FAILED`, `0` on in-flight (CREATED/IN_PROGRESS) so scripts can distinguish via `status` field.
- All M2 contract flags supported.

## Context / Notes

- Endpoints: https://docs.perplexity.ai/api-reference/async-chat-completions-post
- Depends on M2.
- Budget: ~400 LoC (two commands + polling-aware client method + tests).
- Deliberate split: blocking helper lives in M6, not here, to keep this milestone under cap.
