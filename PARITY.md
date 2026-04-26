# Parity Matrix — `perplexity` CLI

Maps upstream **Perplexity Sonar API** endpoints and the **official Perplexity MCP server** tools (`@perplexity-ai/mcp-server`) to the `perplexity` CLI surface.

- **Last audited:** 2026-04-26 (published-artifact smoke clean: brew cask `sapihav/tap/perplexity` v0.6.0 — `version`, `--help`, `schema` all pass)
- **Sources:**
  - API reference: <https://docs.perplexity.ai/api-reference> (+ <https://docs.perplexity.ai/llms.txt>)
  - Official MCP: <https://github.com/perplexityai/modelcontextprotocol> (npm `@perplexity-ai/mcp-server`)
  - CLI commands: `perplexity schema` (Go binary, M1+M2 shipped)
  - Backlog: [`docs/backlog/_index.md`](docs/backlog/_index.md)

> **MCP note:** Unlike Exa/Tavily (no first-party MCP at audit time), Perplexity **does** ship an official MCP server with 4 tools. CLI parity tracks both API and MCP coverage.

## Capability Matrix

| API endpoint | MCP tool | CLI command | Status | Notes |
|---|---|---|---|---|
| `POST /v1/chat/completions` (Sonar chat) | `perplexity_ask` | `perplexity ask <query>` | shipped | M2. `--model sonar\|sonar-pro`, `--system`, `--messages @file`, `--max-tokens`. |
| `POST /v1/chat/completions` w/ `sonar-reasoning-pro` | `perplexity_reason` | `perplexity reason <query>` | shipped (M4) | `--model` (default `sonar-reasoning-pro`), `--system`, `--messages @file`, `--max-tokens`, `--strip-thinking` (default true → splits `<think>` block into `result.thinking`). |
| `POST /async/chat/completions` (submit) | part of `perplexity_research` | `perplexity research submit <prompt>` | shipped (M5) | Async core for `sonar-deep-research`. `--reasoning-effort low\|medium\|high` (default `medium`), `--system`, `--messages @file`, `--max-tokens`. |
| `GET /async/chat/completions/{id}` | part of `perplexity_research` | `perplexity research get <id>` | shipped (M5) | Returns `result.{job_id, status, answer?, reasoning?, model, created_at, completed_at?, failed_at?, error_message?}` + `citations[]`. Exits 0 on COMPLETED / in-flight, 1 on FAILED. |
| `GET /v1/async/sonar` (list) | — | `perplexity jobs list` | planned (M6) | [`add-research-run-and-jobs-list.md`](docs/backlog/tasks/add-research-run-and-jobs-list.md). P2, depends on M5. |
| (composite of submit + poll) | `perplexity_research` (blocking) | `perplexity research run <prompt>` | planned (M6) | Blocking helper; matches MCP `perplexity_research` semantics. |
| `POST /v1/search` (ranked web search) | `perplexity_search` | `perplexity search <query>` | shipped | M1. `--max-results`, `--domain`, `--exclude-domain`, `--recency`, `--country`, `--language`, `--date-from/--date-to`. |
| `POST /v1/agent` (Agent API, web_search/fetch_url/function tools) | — | — | skipped (out of scope) | New Agent API surface. Not in ROADMAP §5.1. Reconsider if demand. |
| `GET /v1/models` | — | — | skipped (low value) | Model list is small + stable; document in README instead. |
| `POST /v1/embeddings` | — | — | skipped (out of scope) | Embeddings are not search/answer; ROADMAP scopes this CLI to Sonar. |
| `POST /v1/embeddings/contextualized` | — | — | skipped (out of scope) | Same as above. |
| `POST /v1/auth/token` | — | — | n/a | CLI uses `PERPLEXITY_API_KEY` env only (ROADMAP §4). |
| `POST /v1/auth/token/revoke` | — | — | n/a | Same as above. |
| — | — | `perplexity schema` | shipped | Self-describing command tree (workspace contract). |
| — | — | `perplexity version` | shipped | Standard version command. |
| (enrichment params on chat/async) | (none) | `--web-search-options`, `--search-domain-filter`, `--search-recency-filter`, `--return-images`, `--return-related-questions` on `ask`/`reason`/`research` | planned (M7) | [`add-enrichment-flags.md`](docs/backlog/tasks/add-enrichment-flags.md). P3, depends on M5. |

## Counts

- **API endpoints:** 11 (6 shipped, 2 planned, 5 skipped, 2 n/a auth)
- **MCP tools (official):** 4 — `perplexity_search`, `perplexity_ask`, `perplexity_research`, `perplexity_reason` (3 mapped shipped via primitives, 1 planned blocking helper)
- **CLI commands shipped:** 7 — `search`, `ask`, `reason`, `research submit`, `research get`, `schema`, `version`
- **CLI commands planned:** 2 — `research run`, `jobs list`

## Gaps

1. **Blocking `research run` + `jobs list` (M6, P2)** — quality-of-life on top of M5. `research run` mirrors MCP `perplexity_research` blocking semantics by polling internally.
2. **Enrichment flags (M7, P3)** — `--return-images`, `--return-related-questions`, `--search-domain-filter`, `--search-recency-filter` on chat/async commands.
3. **Agent API (`/v1/agent`)** — intentionally skipped. Re-evaluate if users ask. Tracked as idea: OpenAI-compat `/v2` Responses surface (see `_index.md` Ideas).
4. **Streaming** — SSE on `ask`/`reason` listed under Ideas, not yet a milestone.
5. **Multimodal input** — `--image` flag idea, blocked on Sonar multimodal GA.

## Out of scope (won't ship)

- Embeddings endpoints — not a search/answer concern.
- Auth token endpoints — CLI is env-key only by design (ROADMAP §4.1).
- `GET /v1/models` — README enumeration is sufficient.
