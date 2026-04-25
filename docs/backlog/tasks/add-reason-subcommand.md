---
title: M4 — `reason` subcommand
type: task
priority: P2
status: done
created: 2026-04-18
---

# M4 — `reason` subcommand

## Problem Statement

MCP exposes `perplexity_reason` backed by `sonar-reasoning-pro` for step-by-step reasoning. The CLI has no equivalent — agents must manually pass `--model sonar-reasoning-pro` to `ask`, and the `<think>...</think>` tokens in the response pollute downstream processing.

## Acceptance Criteria

- `perplexity reason "<query>"` subcommand calling `/chat/completions` with `sonar-reasoning-pro`.
- `--strip-thinking` flag (default `true`) removes `<think>...</think>` blocks from `result.answer`. When `false`, raw content is returned plus a parsed `result.thinking` string.
- Supports `--system`, `--messages @file.json`, stdin `-`, and all M2 contract flags.
- Envelope: `result.{answer, thinking?, model}` + `citations[]`.

## Context / Notes

- Depends on M2 (shared request builder + contract flags).
- Budget: ~250 LoC.
- Edge case: some Sonar responses use `<thinking>` instead of `<think>`. Support both via regex.
