# AGENTS.md — brave-search

## What this is

A CLI and local MCP server over three Brave Search API endpoints: Web Search
(ranked results with snippets), LLM Context (page chunks pre-extracted for a
model's context) and Answers (answers grounded and cited by Brave, in a
single-search mode and a multi-step research mode). It is a search
primitive: it returns what Brave returns and never stores, recomposes or
redistributes it. gem-search (util-series) is the agentic *report generator*
on Vertex Grounding; this is the *search call* that needs no GCP.

Module path: `github.com/nlink-jp/brave-search`.

## Build & test

```bash
make build   # → dist/brave-search  (NEVER `go build` directly — it drops the binary in the repo root)
make test    # go test -race -cover ./...   (fully offline)
make e2e     # live tests against the real Brave API (network + key required)
make check   # lint + test + build-all
make verify-release  # gate: .notarized marker + freshness (run before upload)
```

Go 1.25.0, standard library only — `go.mod` has no `require` block. Shared
code (the release scripts, the sectioned-TOML reader, the `internal/mcp`
skeleton) is vendored from sibling projects rather than imported.

## Tests

**Offline suite** (`make test`): every test sets `HOME` and `XDG_CONFIG_HOME`
to a tempdir and clears every `BRAVE_SEARCH_*` variable, so the suite never
touches the developer's real config or key. Upstream is an `httptest.Server`
reached through `BRAVE_SEARCH_BASE_URL`; the MCP layer is driven by an
in-process dummy JSON-RPC client.

**Fixtures are synthetic, and must stay so.** The Brave ToS forbids storing
Search Results beyond transient operational use, and defines them to include
Answers text and third-party content. A fixture is written by hand after the
*format* (JSON shape, SSE line structure, tag names) has been confirmed
against the live API; its titles, URLs, snippets and answer text are
invented. Never paste a live response into the repository.

`internal/mcp/usage_test.go` holds meta-tests that pin `usage.md` to the code
— every tool name, every argument and every error code must appear in the
manual, because the manual is what an agent reads before its first call.

**Live suite** (`make e2e`, network + key required, excluded from
`go test ./...` by the `e2e` build tag): `e2e/live_test.go` asserts on live
responses and logs every measurement with a `MEASURED:` prefix — those lines
answer the open questions below and belong in Gotchas with a date. Research
is opt-in (`BRAVE_SEARCH_E2E_RESEARCH=1`) because it is billed per search.
`scripts/e2e.sh` drives the built binary. Budget: about 11 requests per full
run, 12 with research. Nothing is saved.

## Layout

```
main.go                      Entry point; delegates to internal/app
internal/config/             Sectioned-TOML subset + BRAVE_SEARCH_* env; local range validation
internal/brave/              Upstream client (web / context / answers SSE), structured errors
internal/engine/             Defaults, validation, response shaping — shared by CLI and MCP
internal/app/                CLI shell: dispatch, flags, text/JSON rendering, auth check
internal/mcp/                Zero-dep stdio JSON-RPC 2.0 server + embedded usage.md
e2e/                         Live tests behind the `e2e` build tag
docs/{en,ja}/                RFP (the design record) + project ADRs
```

## Key design decisions

- **Brave, despite gem-search's rejection of it** — ADR-0001. The use is a
  primitive, not a recomposing report generator; the 2026-09-01 ToS forbids
  storage / redistribution / training but not inference-time use; the plans
  are subscribed.
- **No disk cache.** ToS §3(b)(i). The lookup family's TTL cache is not ported.
- **Answers is always streamed** internally and assembled into one response:
  citations and research mode require `stream=true`.
- **research defaults are tighter than the API's** (10 queries / 2 iterations
  / 120 s vs 20 / 4 / 180): one call bills every search plus tokens.
- **Config precedence is flag > env > file > default**, keys are strict
  (unknown = error), ranges are validated before any request is spent.
- **`Api-Version` is sent only when configured.** Whether an arbitrary date is
  accepted is unverified; until measured, the default is "latest".
- **`notifications/cancelled` is received and ignored.** The MCP spec defines
  it (since 2024-11-05) and allows a receiver to ignore it when the request
  cannot be cancelled; a dispatched upstream search cannot, and is billed
  regardless.

## Gotchas

Nothing has been measured against the live API yet. `auth check` rests on a
documentation claim — that Brave refuses an invalid request with 4xx *after*
checking the key, and bills no failed response — which
`TestLiveProbeDistinguishesKeyFromRequest` pins. The open questions, answered
by `make e2e` and recorded here with a date:

1. ~~Does one key span the Search and Answers plans?~~ **Answered
   2026-09-12 (operator, from the account dashboard): one key per plan.**
   `api_key` serves web/context, `answers_api_key` serves answer/research,
   and neither is sent to the other's endpoint.
2. Does `/chat/completions` return `X-RateLimit-*` headers?
3. Do Answers / LLM Context fetch target pages live, or serve from Brave's
   index? (Decides the mcp-tactics tier.)
4. What does a research call actually cost at the defaults?

## Conventions (organization-wide)

See `../CLAUDE.md` and the organization [CONVENTIONS.md](https://github.com/nlink-jp/.github/blob/main/CONVENTIONS.md).

- Tests are mandatory; design for testability (pure functions, injected deps).
- Small, typed commits (`feat:` / `fix:` / `docs:` / `chore:` / `test:` / `refactor:`).
- README.md and README.ja.md update in the same commit as behavior changes.
- No secrets, no PII, no infra values ever committed.
