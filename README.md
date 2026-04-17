# perplexity-cli

Thin Go CLI wrapping the [Perplexity](https://www.perplexity.ai/) Sonar API. Returns AI answers with citations as structured JSON — agent-friendly, pipeable, no hidden state.

Binary name: `perplexity` (not `perplexity-cli`).

## Status

Milestone 1 — single subcommand: `search`. See `docs/backlog/` for what's next.

## Install

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

```sh
perplexity search "what is the capital of France?"
```

Example output (compact by default, one JSON object per invocation):

```json
{"answer":"The capital of France is Paris.","model":"sonar","citations":["https://en.wikipedia.org/wiki/Paris","https://www.britannica.com/place/Paris"]}
```

Pretty-printed:

```sh
perplexity search "what is the capital of France?" --pretty
```

```json
{
  "answer": "The capital of France is Paris.",
  "model": "sonar",
  "citations": [
    "https://en.wikipedia.org/wiki/Paris",
    "https://www.britannica.com/place/Paris"
  ]
}
```

## Flags

| Flag | Default | Purpose |
|------|---------|---------|
| `--model` | `sonar` | Perplexity model name |
| `--max-retries` | `3` | Retries on 429/5xx with exponential backoff |
| `--pretty` | off | Indent JSON output |
| `--out <file>` | — | Write JSON to a file instead of stdout |
| `--verbose`, `-v` | off | Progress logs to stderr |
| `--quiet`, `-q` | off | Suppress non-error stderr output |

## Output contract

- **stdout**: one JSON object per invocation with `answer`, `model`, `citations` (array of URL strings).
- **stderr**: human-readable progress / errors only.
- **Exit codes**: `0` success, `1` API error (HTTP ≥ 400 after retries), `2` user/config error (e.g., missing env var), `3` network error.

## Development

```sh
go test ./...
go test -cover ./internal/client/...
go build -o perplexity .
```

## License

MIT — see `LICENSE` (to be added).
