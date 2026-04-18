---
title: M7 — enrichment flags across commands
type: task
priority: P3
status: todo
created: 2026-04-18
---

# M7 — enrichment flags across commands

## Problem Statement

Perplexity's chat/completions endpoint supports several optional enrichments (images, related questions, recency filters, domain filters) that are valuable to agents but not surfaced by any subcommand. These are flag-only additions — no new subcommand required.

## Acceptance Criteria

- On `ask`, `reason`, and `research submit` / `research run`:
  - `--return-images` — include image URLs in envelope under `result.images[]`.
  - `--return-related-questions` — include under `result.related_questions[]`.
  - `--search-recency DURATION` (e.g. `1d`, `1w`, `1m`, `1y`).
  - `--search-domain DOMAIN` (repeatable).
- Surface optional `cost_usd` in the envelope when the API returns `usage` block with pricing.
- Updated `schema` output reflects new flags.

## Context / Notes

- Depends on M2 and M5.
- Budget: ~200 LoC (mostly request-struct + envelope additions + test updates).
- Consolidate cost/usage extraction into shared helper to avoid drift across commands.
