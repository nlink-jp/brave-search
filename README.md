# brave-search

**The Brave Search API — web results, LLM-ready page context, and grounded answers — as a CLI and a local MCP server.**

brave-search exposes three [Brave Search API](https://brave.com/search/api/) endpoints:

| Command / tool | Endpoint | What you get |
|---|---|---|
| `web` / `web_search` | Web Search | ranked results with title, URL, snippet and freshness |
| `context` / `llm_context` | LLM Context | page text pre-extracted and sized to a token budget, for grounding a model |
| `answer` / `answer` | Answers | a grounded answer with citations, from one search |
| `research` / `research` | Answers (research mode) | a multi-step, multi-search answer with citations and declared blind spots |

It is a search primitive: it returns what Brave returns, prints what the call cost, and never stores, recomposes or redistributes a result. [gem-search](https://github.com/nlink-jp/gem-search) is the agentic report generator on Vertex AI; this is the search call that needs only a Brave API key.

## Install

```bash
make build  # → dist/brave-search
```

## Usage

```bash
# Ranked results — the query need not be quoted; flags may follow it
brave-search web go generics --count 5
brave-search web -- go -tutorial                    # a leading "-" (exclusion) needs "--" first
brave-search web "site:go.dev generics" --freshness pm --extra-snippets
brave-search web --json go generics --full          # every upstream field, as JSON

# Page text sized to a token budget, for grounding a model
brave-search context "how do go generics work" --max-tokens 2048 --max-urls 5

# Country / language / safesearch apply to every command
brave-search web 生成AI --country jp --lang ja

# A grounded answer with citations, from one search
brave-search answer "what changed in go 1.25"

# Research mode: several searches over several iterations, with the blind
# spots Brave declares. The expensive one — progress is printed to stderr
brave-search research "compare alpha and beta for use case X" --max-queries 5 --max-iterations 1

# As an MCP server (stdio) — tools: web_search, llm_context, answer, research, get_usage
brave-search mcp
```

Every result ends with what the call cost and the rate budget left:

```
cost: $0.0050 (list-price estimate) · requests: 1 · rate budget left: 0/1, 1999/2000
```

The Search endpoints are billed per request at Brave's published price, so the figure is an estimate; the Answers endpoints report their exact cost:

```
cost: $0.0157 (reported by Brave) · requests: 1 · searches: 2 · tokens: 1234 in / 300 out
```

**An answer costs about ten times a web search.** Brave feeds the search results to its model and bills them as input tokens — a single `answer` measured at about 10,000 input tokens and $0.054–0.058. **Citations are unreliable when the requested reply language is not English** (measured on one question: `--lang en` had them in every run, `--lang ja` in one run of three). An `answer` requested in another language that comes back without citations says so in a `note`; ask again, or with `--lang en`, when sources matter.

`research` is deliberately capped below the API's own defaults (10 searches per iteration, 2 iterations, 120 seconds against 20 / 4 / 180): one call bills every search it runs plus tokens, and it cannot be stopped once dispatched.

### Configuration

`~/.config/brave-search/config.toml` — see [config.example.toml](config.example.toml) for every key. Precedence is flag > environment variable > file > built-in default. Unknown keys are rejected. The built-in country and language defaults are Brave's own (`US`, `en`); set `country = "JP"` and `search_lang = "ja"` for Japanese results.

### MCP server

Register `brave-search mcp` with your client, for example in Claude Code:

```bash
claude mcp add brave-search -- /path/to/brave-search mcp
```

Call `get_usage` first — it is the full reference for the tools, their result schemas, the cost model and the error codes.

## Setup

1. Subscribe at <https://api-dashboard.search.brave.com/> — the **Search** plan covers `web` and `context`, the **Answers** plan covers `answer` and `research` — and create an API key.
2. Brave issues **one key per plan**. Put the Search key in `~/.config/brave-search/config.toml` as `api_key` (or `BRAVE_SEARCH_API_KEY`) and the Answers key as `answers_api_key` (or `BRAVE_SEARCH_ANSWERS_API_KEY`) — see [config.example.toml](config.example.toml). Keys are never accepted as flags.
3. `brave-search auth check` reports each key's standing — valid, rejected, plan not subscribed, or absent — and which config file was read. It probes each plan with a deliberately invalid request; Brave documents that failed responses are not billed, so the check should be free.

## What the Terms of Service mean for this tool

Brave's [API terms](https://api-dashboard.search.brave.com/documentation/resources/terms-of-service) permit only transient retention of search results, and forbid redistributing them or using them to train, evaluate or improve AI models. Consequently:

- **Nothing is cached.** An identical call is a second billed request.
- **Every result reports its cost** and the rate budget left, so spend is visible.
- Results are Brave's and their sources'. Cite the URLs; do not store or redistribute them, and do not feed them into model training or evaluation. Using them as a model's input at inference time is what the LLM Context and Answers endpoints exist for.

## Attribution

The [official Brave Search skills](https://github.com/brave/brave-search-skills) (MIT) were the specification reference for the endpoints. No code was taken from them.

## License

MIT — see [LICENSE](LICENSE).
