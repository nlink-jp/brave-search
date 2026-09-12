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

> **Status: under development.** The scaffold builds and tests; the search commands are not implemented yet.

## Install

```bash
make build  # → dist/brave-search
```

## Setup

1. Subscribe at <https://api-dashboard.search.brave.com/> — the **Search** plan covers `web` and `context`, the **Answers** plan covers `answer` and `research` — and create an API key.
2. Put the key in `~/.config/brave-search/config.toml` (see [config.example.toml](config.example.toml)) or in `BRAVE_SEARCH_API_KEY`. The key is never accepted as a flag.
3. `brave-search auth check` tells you which plans the key unlocks.

## What the Terms of Service mean for this tool

Brave's [API terms](https://api-dashboard.search.brave.com/documentation/resources/terms-of-service) permit only transient retention of search results, and forbid redistributing them or using them to train, evaluate or improve AI models. Consequently:

- **Nothing is cached.** An identical call is a second billed request.
- **Every result reports its cost** and the rate budget left, so spend is visible.
- Results are Brave's and their sources'. Cite the URLs; do not store or redistribute them, and do not feed them into model training or evaluation. Using them as a model's input at inference time is what the LLM Context and Answers endpoints exist for.

## Attribution

The [official Brave Search skills](https://github.com/brave/brave-search-skills) (MIT) were the specification reference for the endpoints. No code was taken from them.

## License

MIT — see [LICENSE](LICENSE).
