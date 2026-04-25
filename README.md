# perplexity-cli

Thin Go CLI wrapping the [Perplexity](https://www.perplexity.ai/) Sonar API. Returns AI answers with citations as structured JSON — agent-friendly, pipeable, no hidden state.

Binary name: `perplexity` (not `perplexity-cli`).

## Status

Milestone 4 shipped — added `reason` (chain-of-thought via
`sonar-reasoning-pro`, with optional `<think>` block extraction). M3 brought
the standalone `POST /search` endpoint; `ask` keeps chat-completions
synthesis. All M2 contract flags (`--dry-run`, `--timeout`, `--json-errors`,
`--rate-limit`, `--user-agent`, stdin `-`) apply to every subcommand. See
`docs/backlog/` for what's next and `CHANGELOG.md` for release notes.

## Install

**Homebrew (macOS)** — recommended on Mac:

```sh
brew install sapihav/tap/perplexity
```

The tap auto-installs on first use; subsequent `brew upgrade` picks up new releases.

**One-line install (Linux / macOS)** — no Go toolchain required:

```sh
curl -sSL https://raw.githubusercontent.com/sapihav/perplexity-cli/main/install.sh | bash
```

Downloads the latest release for your OS/arch, verifies SHA-256, installs `perplexity` to `/usr/local/bin`. Override with `INSTALL_DIR=$HOME/.local/bin`. Requires `curl` + `jq`.

**From source** (requires Go 1.25+):

```sh
go install github.com/sapihav/perplexity-cli@latest
```

This drops a `perplexity` binary in `$(go env GOPATH)/bin`.

## Auth

Set your Perplexity API key (create one at https://www.perplexity.ai/settings/api):

```sh
export PERPLEXITY_API_KEY=pplx-xxxxxxxxxxxxxxxx
```

There is no config-file fallback. Missing env var exits with code `2`.

## Usage

### `perplexity search` — ranked web results (no AI synthesis)

Wraps `POST /search` and returns a list of ranked web results. No answer,
no citations — those live on `ask`.

```sh
perplexity search "what is the capital of France?"
perplexity search "llm evals 2026" --max-results 20 --recency 1w
perplexity search "raft consensus" --domain wikipedia.org --language en --country US
perplexity search "ipo filings" --date-from 2024-01-01 --date-to 2024-12-31
echo "nvidia earnings q4" | perplexity search -
```

Flags (on `search`): `--max-results N` (1–20, default 10), `--country ISO`,
`--language ISO` (ISO 639-1, repeatable, ≤10), `--domain DOMAIN` (repeatable,
≤20), `--exclude-domain DOMAIN` (repeatable, ≤20; mutually exclusive with
`--domain`), `--recency 1h|1d|1w|1m|1y` (mutually exclusive with
`--date-from`/`--date-to`), `--date-from YYYY-MM-DD`, `--date-to YYYY-MM-DD`.

### `perplexity ask` — conversational answers (sonar-pro by default)

```sh
perplexity ask "summarise the Raft consensus algorithm"
perplexity ask "continue the analysis" --messages @history.json
echo "what changed between Go 1.24 and 1.25?" | perplexity ask -
perplexity ask "rewrite this prompt" --system "You are a terse editor" --max-tokens 400
```

`--messages @file.json` accepts a prior conversation as an array of
`{role, content}` objects; the positional query is appended as the final
user turn.

### `perplexity reason` — step-by-step reasoning (sonar-reasoning-pro)

```sh
perplexity reason "if a train leaves Paris at 9am at 200km/h, when does it reach Lyon?"
perplexity reason "explain RCU vs spinlocks" --strip-thinking=false   # keep raw <think> in answer
perplexity reason "tricky math problem" --model sonar-reasoning       # smaller reasoning model
echo "why is the sky blue?" | perplexity reason -
```

The reasoning models emit a `<think>...</think>` chain-of-thought block followed by the final answer in the same content stream. By default the CLI splits them: the cleaned final answer goes to `result.answer`, the chain-of-thought to `result.thinking`. Pass `--strip-thinking=false` to receive the raw content unchanged (and no `thinking` field).

`reason` output payload: `{answer, thinking?, model, citations[]}`.

### `perplexity schema` — self-describing command tree

```sh
perplexity schema | jq .
```

Returns the full command tree (flags, defaults, subcommands) as JSON.
Agents should prefer this over scraping `--help`.

Example `search` output (compact by default, one JSON envelope per invocation):

```json
{"schema_version":"1","provider":"perplexity","command":"search","elapsed_ms":1234,"result":{"results":[{"title":"Paris","url":"https://en.wikipedia.org/wiki/Paris","snippet":"Paris is the capital of France.","published_date":"2024-01-15","domain":"en.wikipedia.org"}]}}
```

Pretty-printed:

```sh
perplexity search "what is the capital of France?" --pretty
```

```json
{
  "schema_version": "1",
  "provider": "perplexity",
  "command": "search",
  "elapsed_ms": 1234,
  "result": {
    "results": [
      {
        "title": "Paris",
        "url": "https://en.wikipedia.org/wiki/Paris",
        "snippet": "Paris is the capital of France.",
        "published_date": "2024-01-15",
        "domain": "en.wikipedia.org"
      }
    ]
  }
}
```

## Flags

### Per subcommand

| Flag | Scope | Default | Purpose |
|---|---|---|---|
| `--max-results N` | `search` | `10` | Number of ranked results (1–20) |
| `--country ISO` | `search` | unset | Regional bias (e.g. `US`, `GB`) |
| `--language ISO` | `search` | unset | ISO 639-1 language filter (e.g. `en`); repeatable, up to 10 entries |
| `--domain DOMAIN` | `search` | unset | Restrict to domain (repeatable) |
| `--exclude-domain DOMAIN` | `search` | unset | Exclude domain (repeatable) |
| `--recency DURATION` | `search` | unset | `1h`, `1d`, `1w`, `1m`, `1y` |
| `--date-from YYYY-MM-DD` | `search` | unset | Earliest publish date |
| `--date-to YYYY-MM-DD` | `search` | unset | Latest publish date |
| `--model` | `ask` | `sonar-pro` | Model name for ask |
| `--max-tokens N` | `ask` | unset | Cap response tokens |
| `--system <prompt>` | `ask` | unset | System prompt |
| `--messages @file.json` | `ask` | unset | Prior conversation (array of `{role,content}`) |
| `--model` | `reason` | `sonar-reasoning-pro` | Reasoning model (`sonar-reasoning-pro` \| `sonar-reasoning`) |
| `--max-tokens N` | `reason` | unset | Cap response tokens |
| `--system <prompt>` | `reason` | unset | System prompt |
| `--messages @file.json` | `reason` | unset | Prior conversation (array of `{role,content}`) |
| `--strip-thinking` | `reason` | `true` | Strip `<think>...</think>` from `result.answer` and surface it under `result.thinking` |

### Global (persistent on every subcommand)

| Flag | Default | Purpose |
|---|---|---|
| `--dry-run` | off | Print the planned request (`Authorization` redacted) and exit without calling the API |
| `--timeout SEC` | `60` | Per-request timeout in seconds |
| `--rate-limit N/s` | `1` | Client-side rate limit (0 disables) |
| `--max-retries N` | `3` | Retries on `429` / `5xx` with exponential backoff |
| `--json-errors` | off | Emit errors as `{error:{message,code}}` on stderr |
| `--user-agent STR` | `perplexity-cli/…` | Override `User-Agent` header (also via `PERPLEXITY_USER_AGENT`) |
| `--pretty` | off | Indent JSON output |
| `--out <file>` | — | Write JSON to a file instead of stdout |
| `--verbose`, `-v` | off | Progress logs to stderr |
| `--quiet`, `-q` | off | Suppress non-error stderr output |

Any subcommand that takes a query also accepts `-` to read from stdin.

## Output contract

- **stdout** (success): one JSON envelope per invocation — `{schema_version, provider, command, elapsed_ms, result}`. Per-command payload under `result`: `search` → `{results[]{title,url,snippet,published_date,domain}}`; `ask` → `{answer, model, citations[]}`; `reason` → `{answer, thinking?, model, citations[]}`.
- **stderr**: human-readable progress / errors only. No envelope on the error path.
- **Exit codes**: `0` success, `1` API error (HTTP ≥ 400 after retries), `2` user/config error (e.g., missing env var), `3` network error.

## Development

```sh
go test ./...
go test -cover ./internal/client/...
go build -o perplexity .
```

## License

MIT — see [`LICENSE`](./LICENSE).
