# CLAUDE.md — brave-search

**Organization rules (mandatory): https://github.com/nlink-jp/.github/blob/main/CONVENTIONS.md**

## Purpose

CLI + local MCP server exposing three Brave Search API endpoints — **Web
Search** (`/web/search`), **LLM Context** (`/llm/context`) and **Answers**
(`/chat/completions`, single-search and research mode) — as search
primitives. It returns what Brave returns; it never stores, recomposes or
redistributes it. Design record: [docs/ja/brave-search-rfp.ja.md](docs/ja/brave-search-rfp.ja.md);
why Brave at all, given gem-search rejected it: [ADR-0001](docs/ja/adr/0001-adopt-brave-search-api.ja.md).

## Build & test

```bash
make build       # → dist/brave-search  (never `go build` directly — it drops the binary in the repo root)
make test        # go test -race -cover ./...   (fully offline)
make e2e         # live tests against the real Brave API (network + key required)
make check       # lint + test + build-all
```

Go 1.25+. **No external dependencies — standard library only.**

## Architecture

```
main.go                 CLI entry: main.version → app.Run
internal/config/        Sectioned-TOML subset + BRAVE_SEARCH_* resolution; local range validation
internal/brave/         Upstream client: web search, LLM context, answers (SSE) — net/http only
internal/engine/        Defaults + validation + response shaping; shared by CLI and MCP
internal/app/           Dispatch + web/context/answer/research/auth/mcp; text and JSON rendering
internal/mcp/           Zero-dep stdio JSON-RPC 2.0 server; embedded get_usage manual
e2e/                    Live tests behind the `e2e` build tag
```

## Key conventions

- **No disk cache, no live-response fixtures — this is the ToS, not a
  preference.** §3(b)(i) permits only transient retention of results, and
  "Search Results" includes Answers text. There is no `cache` subcommand and
  no `[cache]` section; every test fixture is synthetic — content invented,
  format confirmed against the live API on 2026-09-12 for web, context and
  single-search answers (research's stream shape is still a transcription of
  the documentation). Do not add either.
- **The parameter matrix in the RFP is the single mapping** of CLI flag ↔ MCP
  argument ↔ config key ↔ upstream parameter. Add a parameter there first;
  the three surfaces derive from it.
- **Every result carries `meta` with cost and rate budget.** Every call is
  billed and nothing is cached; the tool never hides what a call spent.
- **Answers is always called with `stream=true`** and the SSE assembled into
  one response, because citations and research mode exist only in streaming
  mode. Tag parsing lives in the client; the CLI and MCP see structures.
- **`research` progress goes to stderr** while it runs. A silent five-minute
  wait is indistinguishable from a hang.
- **Brave's stream tags have no escaping.** JSON-carrying tags (citation,
  usage, progress) are honoured only when their body is a JSON object, so
  literal `<progress>` in prose survives; text-carrying tags (answer,
  blindspots, thinking, queries, analyzing) cannot be told apart and are
  stripped. Known limit, recorded in AGENTS.md.
- **Ranges are validated locally** before a request is spent. The upstream
  ranges are constants in `internal/config`; re-verify them against the
  documentation when they change.
- **The key is a secret.** Sent only in `X-Subscription-Token`; never in a
  URL, a flag or a log. `config.toml` is gitignored.
- **`notifications/cancelled` is received and ignored.** The spec allows it
  when a request cannot be cancelled, and a dispatched upstream search cannot.
  Do not describe this as "the spec has no cancel".
- **Engine is shared** by CLI and MCP so their behaviour cannot diverge.

## Status

Phase 1 (scaffold) — see CHANGELOG.md. Live measurements are recorded, dated,
in AGENTS.md as they are made.

## Communication Language

All communication between contributors and Claude Code is conducted in
**Japanese**.
