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
Answers text and third-party content. A fixture is written by hand; its
titles, URLs, snippets and answer text are invented. The *format* (JSON shape,
SSE line structure, tag names) was confirmed against the live API on
2026-09-12 for web search, LLM context, single-search answers and research.
Never paste a live response into the repository.

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
- **`Api-Version` is sent only when configured.** An arbitrary date is
  rejected with 404 (measured), so the default is "latest".
- **`notifications/cancelled` is received and ignored.** The MCP spec defines
  it (since 2024-11-05) and allows a receiver to ignore it when the request
  cannot be cancelled; a dispatched upstream search cannot, and is billed
  regardless.
- **Every tool schema is closed** (`additionalProperties: false`, organization
  ADR-021 §10), and both halves of the contract are real: the schema stops a
  mistyped argument at a validating client, `mcp.DecodeArgs` stops it at the
  server. `closeSchema` is applied inside `mcp.New` — the one place every tool
  passes through — so a tool added later cannot forget it; only the top level
  is touched. The arch tests live in `internal/app/schema_test.go`, not
  `internal/mcp`, for the same reason the manual meta-tests do: `internal/mcp`
  on its own sees `get_usage` alone, so asserting there would check one tool
  and miss four.

## Gotchas

Measured against the live API on 2026-09-12. Re-verify before trusting any of
this a year from now.

- **An answer costs ~10× a web search.** One single-search `answer` was
  billed as 1 search + ~9,800–10,500 input tokens + ~200–300 output tokens =
  **$0.054–0.058**; Brave feeds the retrieved pages to its model and bills
  them as input. A web search is $0.005. Say so wherever an agent chooses.
- **Citations are unreliable for non-English replies — and not
  deterministic.** One question, country constant, five runs: `en` → 29, 29;
  `ja` → 0, 0, 24. So "no citations" is a per-run outcome that clusters on
  non-English *requests* (the predicate is the `language` parameter, `en`
  and its regional variants counting as English), not a rule. The engine
  attaches a `note` to a single-search `answer` requested in another
  language that has no citations — not to `research`, which returned none
  in English too. An empty `citations` never means "no sources exist".
  Other languages unchecked.
- **The Answers endpoint returns no `X-RateLimit-*` headers**; the Search
  endpoints do. Answers results therefore carry no `rate_limit`.
- **A key of the other plan is accepted.** A billed `/web/search` with the
  Answers key returned 200 with the Answers plan's rate policy (`2;w=1`), so
  keys are not endpoint-locked — they select which plan is billed and
  throttled. The tool still sends each plan its own key; a 403
  `plan_not_subscribed` has never been observed.
- **A pay-as-you-go key's monthly window is `0`** — policy `50;w=1,
  0;w=2592000`, remaining `49, 0`. A 0 limit means uncapped, not exhausted;
  `RateLimit.Capped` and `ResetSeconds` skip such windows, and the CLI prints
  "uncapped". Reading it as exhausted would say "wait 18 days" on every 429.
- **`Api-Version` accepts only published versions.** `Api-Version:
  2026-09-12` → 404 "The requested product api version is not found." The
  default therefore sends no header (latest); the RFP's "pin the
  implementation date" is withdrawn. Which dates are published is not
  documented where we looked.
- **Brave's stream tags have no escaping.** A literal `<progress>` or
  `<citation>` in prose is indistinguishable from a control tag by brackets
  alone. The parser honours JSON-carrying tags only when their body is a JSON
  object (a literal opener is skipped one tag at a time, so it cannot pair
  with a real closer), and strips text-carrying tags (`answer`, `blindspots`,
  `thinking`, `queries`, `analyzing`) as Brave's — a question about "how
  `<thinking>` tags work" loses that span. Known limit.
- **Research mode's `<answer>` body is a JSON object, not prose** —
  `{"answer": "…"}` in both live runs (2026-09-12). The parser unwraps it,
  reads `citations` / `blindspots` keys if present (documented, not yet
  observed — both runs had neither, and no `<blindspots>` tag either), and
  keeps any other key verbatim in `answer_extra` (an object with no string
  `answer` leaves `answer` empty and the whole object there). `tags_seen`,
  present in every JSON result (CLI `--json` and MCP), records what the
  stream actually carried.
- **Research progress keys (Brave's spelling included):** `elasped_seconds`,
  `number_of_input_tokens`, `number_of_output_tokens`, `number_of_thinking_tokens`,
  `number_of_iterations`, `number_of_queries`, `number_of_urls_analyzed`,
  `number_of_snippets_analyzed`. One report per iteration.
- **Research cost at 2 queries / 1 iteration / 60 s: $0.063–0.064, ~10–12 s,
  and Brave ran only 1 query of the 2 allowed** (~11,000 input tokens, ~30
  URLs analysed). The caps are ceilings, not targets.
- **Every stream ends with `data: [DONE]`.** A stream that ends without it is
  reported as `upstream_error` ("incomplete"), never as a complete answer.
- **A single-search citation observed live had `start_index == end_index`**
  (165/165, right after the cited sentence): the indexes are insertion points
  in Brave's own text. The engine trims only trailing whitespace so they keep
  their meaning.

- **A bad key is a 422, not a 401.** Brave answers
  `{"error":{"code":"SUBSCRIPTION_TOKEN_INVALID","status":422}}` for an
  invalid `X-Subscription-Token`, and a *missing* header is a 422 `VALIDATION`
  error whose `meta.errors[].loc` is `["header","x-subscription-token"]`. A
  bad request under an accepted key is also 422 `VALIDATION`. The HTTP status
  therefore cannot tell a wrong key from a wrong request; `statusError` reads
  the body's `error.code` first and the status only as a fallback. Every
  endpoint (web, context, answers) behaves the same.
- **The key is checked before the request is validated**, so `auth check`'s
  probe (a request with no query / no message) does distinguish a bad key
  (`SUBSCRIPTION_TOKEN_INVALID`) from a good one (`VALIDATION`) without a
  billable success. That failed responses are unbilled is still a
  documentation claim; watch the dashboard after the first `make e2e`.
- `Api-Version: 2026-09-12` on a bogus-key request was answered by the token
  error, not by a version error — so it was at least not rejected outright.
  Whether it changes the response shape under a valid key is unmeasured.

Open questions still to be answered, recorded here with a date when they are:

1. ~~Does one key span the Search and Answers plans?~~ **2026-09-12: the
   dashboard issues one key per plan, and the API accepts either key on
   either endpoint (see above). The tool keeps them separate.**
2. ~~Does `/chat/completions` return `X-RateLimit-*` headers?~~ **2026-09-12:
   no.** How an unsubscribed plan is refused remains unobserved.
3. Do Answers / LLM Context fetch target pages live, or serve from Brave's
   index? (Decides the mcp-tactics tier.) Not yet checked against Brave's
   documentation of the retrieval path.
4. ~~What does a research call cost, and what tags does its stream carry?~~
   **2026-09-12: see the research bullets above.** Still unobserved: a
   research answer *with* citations or blind spots, so the `citations` /
   `blindspots` keys of the answer object are read on the documentation's
   word only.

## Conventions (organization-wide)

See `../CLAUDE.md` and the organization [CONVENTIONS.md](https://github.com/nlink-jp/.github/blob/main/CONVENTIONS.md).

- Tests are mandatory; design for testability (pure functions, injected deps).
- Small, typed commits (`feat:` / `fix:` / `docs:` / `chore:` / `test:` / `refactor:`).
- README.md and README.ja.md update in the same commit as behavior changes.
- No secrets, no PII, no infra values ever committed.
