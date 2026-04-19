# perplexity-cli

Thin Go CLI wrapping the [Perplexity](https://www.perplexity.ai/) Sonar API. Returns AI answers with citations as structured JSON — agent-friendly, pipeable, no hidden state.

Binary name: `perplexity` (not `perplexity-cli`).

## Status

Milestone 2 shipped — `search`, `ask`, and `schema` subcommands with the
full workspace contract flag set (`--dry-run`, `--timeout`, `--json-errors`,
`--rate-limit`, `--user-agent`, stdin `-`). See `docs/backlog/` for what's next.

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

### `perplexity search` — classic Sonar search

```sh
perplexity search "what is the capital of France?"
```

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

### `perplexity schema` — self-describing command tree

```sh
perplexity schema | jq .
```

Returns the full command tree (flags, defaults, subcommands) as JSON.
Agents should prefer this over scraping `--help`.

Example output (compact by default, one JSON envelope per invocation):

```json
{"schema_version":"1","provider":"perplexity","command":"search","elapsed_ms":1234,"result":{"answer":"The capital of France is Paris.","model":"sonar","citations":["https://en.wikipedia.org/wiki/Paris","https://www.britannica.com/place/Paris"]}}
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
    "answer": "The capital of France is Paris.",
    "model": "sonar",
    "citations": [
      "https://en.wikipedia.org/wiki/Paris",
      "https://www.britannica.com/place/Paris"
    ]
  }
}
```

## Flags

### Per subcommand

| Flag | Scope | Default | Purpose |
|---|---|---|---|
| `--model` | `search` | `sonar` | Model name for search |
| `--model` | `ask` | `sonar-pro` | Model name for ask |
| `--max-tokens N` | `ask` | unset | Cap response tokens |
| `--system <prompt>` | `ask` | unset | System prompt |
| `--messages @file.json` | `ask` | unset | Prior conversation (array of `{role,content}`) |

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

- **stdout** (success): one JSON envelope per invocation — `{schema_version, provider, command, elapsed_ms, result}`. The per-command payload lives under `result` (for `search`: `answer`, `model`, `citations[]`).
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
