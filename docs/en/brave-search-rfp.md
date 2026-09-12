# RFP: brave-search

> Generated: 2026-09-12
> Status: Draft (rev 2 — 16 independent-review findings applied)

## 1. Problem Statement

nlink-jp's agents (Claude Code / gem-agent) and their human operators have
exactly one licence-clean entry point to general web search: gem-search, an
agentic research CLI built on Vertex AI's Google Search Grounding. It runs a
fixed three-phase pipeline (Survey → Deep-dive → Verify) and produces a
research report. That is too heavy for the **search primitive** use cases —
"give me the top ten URLs and snippets for this query", "answer this question
in one paragraph with sources" — and it cannot run at all without a GCP
billing foundation.

brave-search exposes three Brave Search API endpoints — **Web Search** (ranked
results with snippets), **LLM Context** (pre-extracted page chunks for
grounding) and **Answers** (answers grounded and cited by Brave itself, in a
single-search mode and a multi-step research mode) — as both a CLI and an MCP
server. It runs on a Brave API key alone, no GCP. Search results are never
stored or redistributed (Brave ToS §3(b)).

The user is the author (nlink-jp agent operations and research work). Both the
Search and Answers plans are already subscribed.

## 2. Functional Specification

### Commands / API Surface

```
brave-search web <query>              # Web Search: ranked results
brave-search context <query>          # LLM Context: grounding chunks
brave-search answer <question>        # Answers, single-search mode (with citations)
brave-search research <question>      # Answers, research mode (multi-step search)
brave-search auth check               # validate the key and the plans (the only validator)
brave-search mcp                      # run as an MCP server (stdio)
brave-search --version                # version (the brew test calls it)
```

### Parameter matrix (the single mapping of CLI / MCP / config / upstream)

CLI flags, MCP arguments and config keys are **derived from this one table**.
A combination absent here does not exist. Precedence is
**flag > environment > config file > built-in default**.

| Item | CLI | MCP argument | config | Upstream parameter | Applies to | Range / default |
|---|---|---|---|---|---|---|
| Query | positional | `query` (web/context) / `question` (answer/research) | — | `q` / `messages[0].content` | all | web/context: 1–400 chars, ≤ 50 words |
| Count | `--count` | `count` | `[search] count` | `count` | web / context | web 1–20, context 1–50. Default 10 |
| Page | `--offset` | `offset` | — | `offset` | web | 0–9. Default 0 |
| Country | `--country` | `country` | `[search] country` / `[answers] country` | `country` | all | two letters or `ALL`. Default `US` |
| Language | `--lang` | `search_lang` (web/context) / `language` (answer/research) | `[search] search_lang` / `[answers] language` | `search_lang` / `language` | all | Default `en` |
| Freshness | `--freshness` | `freshness` | — | `freshness` | web / context | `pd`/`pw`/`pm`/`py`/`YYYY-MM-DDtoYYYY-MM-DD` |
| Safesearch | `--safesearch` | `safesearch` | `[search] safesearch` / `[answers] safesearch` | `safesearch` | all | `off`/`moderate`/`strict`. Default `moderate` |
| Extra snippets | `--extra-snippets` | `extra_snippets` | — | `extra_snippets` | web | bool. Default false |
| Full fields | `--full` | **none** (MCP is always compact) | — | — | web | pass through the upstream fields dropped by default |
| Token budget | `--max-tokens` | `max_tokens` | `[context] max_tokens` / `[answers] max_tokens` | `maximum_number_of_tokens` / `max_completion_tokens` | context / answer | context 1024–32768, default 8192. answer: API default |
| URL count | `--max-urls` | `max_urls` | `[context] max_urls` | `maximum_number_of_urls` | context | 1–50. Default 20 |
| research queries | `--max-queries` | `max_queries` | `[answers] research_max_queries` | `research_maximum_number_of_queries` | research | 1–50. Default **10** (tighter than the API's 20) |
| research iterations | `--max-iterations` | `max_iterations` | `[answers] research_max_iterations` | `research_maximum_number_of_iterations` | research | 1–5. Default **2** (API 4) |
| research seconds | `--max-seconds` | `max_seconds` | `[answers] research_max_seconds` | `research_maximum_number_of_seconds` | research | 1–300. Default **120** (API 180) |
| Deadline | `--timeout` | **none** | `[api] timeout` | — | all | Default 30s. research derives `max_seconds + 30s` |
| API version | — | — | `[api] api_version` | `Api-Version` header | all | pinned to the implementation date; default is that date |
| Config / output | `--config`, `--json` | — | — | — | all | |

research's per-query token cap (`research_maximum_number_of_tokens_per_query`)
is not exposed in Phase 1 and stays at the API default.

**MCP tools** (share the engine with the CLI, so the two faces cannot give
different answers to the same input; the arguments are the "MCP argument"
column of the matrix for the rows whose "Applies to" includes the tool):

| Tool | Arguments | Returns |
|---|---|---|
| `web_search` | `query`, `count`, `offset`, `country`, `search_lang`, `freshness`, `safesearch`, `extra_snippets` | result array (title / url / description / age / language / extra_snippets) + `query` metadata (original / altered) + `more_results_available` + `meta` (cost, remaining quota) |
| `llm_context` | `query`, `count`, `country`, `search_lang`, `freshness`, `safesearch`, `max_tokens`, `max_urls` | `grounding.generic[]` (url / title / snippets[]) + `sources` + `meta` |
| `answer` | `question`, `country`, `language`, `safesearch`, `max_tokens` | `answer` text + `citations[]` (number / url / snippet / span) + `usage` (searches, tokens, cost) |
| `research` | `question`, `country`, `language`, `safesearch`, `max_queries`, `max_iterations`, `max_seconds` | `answer` + `blindspots` + `citations[]` + `progress` (iterations, queries, URLs analysed) + `usage` |
| `get_usage` | none | the embedded `usage.md` (tool reference and error-recovery table) |

### Input / Output

**Default output (human-readable text)**:

- `web`: rank, title, URL, snippet, freshness (age). When spellcheck rewrote
  the query, `altered` is always shown — never silently answer a different
  query.
- `context`: title and chunks grouped per URL.
- `answer` / `research`: the answer text followed by a numbered source list.
  research always prints `blindspots` (the points Brave declares it could not
  cover) after the answer. While research runs, **progress goes to stderr**
  (iteration, query count, elapsed seconds) — a silent wait of up to 300
  seconds is indistinguishable from a hang.
- All commands: the tail of the output (`meta` under `--json`) carries **the
  cost of this request** (the `<usage>` breakdown for Answers, the flat unit
  price for Search) and `X-RateLimit-Remaining`. Spend is visible on the spot.
  (Whether the Answers endpoint returns `X-RateLimit-*` is measured in
  Phase 1; if not, the Answers `meta` carries only the `<usage>` cost.)

**`--json`**: a stable schema defined by brave-search, not the upstream raw
body. web is compact by default (title / url / description / age / language /
extra_snippets); `--full` passes the upstream fields through. **MCP is always
compact** and has no `full` equivalent (the response-budget face).

**Definition of the response budget**: the budget consists of the explicit
caps only — `count` (web 20 / context 50), `max_tokens`, `max_urls`. There is
**no implicit truncation** beyond them. web is bounded by the compact shape ×
count ≤ 20, context by `max_tokens`, answer / research by the upstream answer
length. There is no server-side spill to files.

**Exit contract** (as otx-lookup / malware-lookup):

| Code | Meaning |
|---|---|
| 0 | the query completed (zero results is a normal answer) |
| 1 | upstream failure, rate limit or timeout — no answer obtained |
| 2 | usage error — bad arguments, bad config, missing API key |

### Configuration

`~/.config/brave-search/config.toml` (sectioned TOML; XDG, and `~/.config` is
searched on macOS too). Keys match the config column of the parameter matrix.

```toml
[api]
api_key     = ""                                   # BRAVE_SEARCH_API_KEY
base_url    = "https://api.search.brave.com/res/v1"
api_version = "2026-09-12"                         # Api-Version header; default is the implementation date
timeout     = "30s"

[search]                    # defaults for web / context
country     = "US"          # Brave's own defaults; set "JP" / "ja" for Japanese search
search_lang = "en"
safesearch  = "moderate"
count       = 10

[context]                   # context only
max_tokens = 8192
max_urls   = 20

[answers]                   # defaults for answer / research
country                = "US"
language               = "en"
safesearch             = "moderate"
max_tokens             = 0             # 0 = leave to the API
research_max_queries    = 10
research_max_iterations = 2
research_max_seconds    = 120
```

Environment variables are `BRAVE_SEARCH_*`. `BRAVE_SEARCH_API_KEY` is the same
name Brave's official skills use, so an environment that already sets it works
unchanged. The API key is accepted only from the file or the environment —
never from a flag (it would land in process listings and shell history).

### External Dependencies

- Brave Search API (`api.search.brave.com`): the Search plan (`/web/search`,
  `/llm/context`) and the Answers plan (`/chat/completions`).
- Go standard library only. Zero external Go dependencies. There is no
  official Go SDK.
- Shared code (release scripts, the sectioned-TOML reader, the `internal/mcp`
  skeleton) is **vendored** from otx-lookup / data-toolbox-mcp, not imported.

## 3. Design Decisions

### Why Brave — reversing the rejection recorded in gem-search

gem-search (2026-04) tried Brave in its predecessor agentic-web-search and
rejected it over the ToS §3(b) restrictions (no storage, redistribution or AI
training) and the paid registration, choosing Vertex AI Web Grounding instead.
This project reverses that for four reasons.

1. **The use differs.** gem-search generates research reports: it feeds
   grounding results to its own LLM and recomposes them. brave-search returns
   **search primitives** and never recomposes, stores or redistributes them.
   No operation in the design touches a prohibited act.
2. **The ToS was re-read** (revision effective 2026-09-01). It forbids (i)
   storage or caching beyond transient operational retention, (ii) derivative
   works, (xii) redistribution or resale, (xiii) using results to train,
   evaluate, benchmark or improve AI models. **No clause restricts use as LLM
   input at inference time**, and LLM Context / Answers are sold for exactly
   that. §4's "Powered by Brave" mark applies only "if Customer elects to
   provide attribution".
3. **The paid registration is done** (user decision). The psychological
   barrier is gone.
4. A search entry point that needs no GCP is needed. gem-search presumes a
   GCP billing foundation.

The reversal is recorded as project ADR-0001, so a future reader sees why this
differs from gem-search's call.

### The structural differences the ToS imposes: no disk cache, no live-response fixtures

The lookup family (otx / gti / abuse …) keeps a TTL'd JSON file cache;
brave-search does not. §3(b)(i) allows only "transient storage required for
operation", and a 24-hour disk cache sits outside that. Retention stays
in-process (assembling a response, accumulating SSE). There is no `cache`
subcommand and no `[cache]` section.

For the same reason **test fixtures never contain live responses**. "Search
Results" is defined to include Generated Results (Answers text) and
Third-Party Content, so freezing bodies captured from the live API in the
repository would be storage (§3(b)(i)) and, in a public repository,
redistribution (§3(b)(xii)). Fixtures are **synthesised** after confirming the
**format** (JSON shape, SSE line structure, tags) against the live API; bodies,
URLs and snippets are invented values.

What may be stored is **our own usage** (request counts, cost) — that is not a
Search Result. Phase 1 only reports it in each response; a ledger is Phase 2.

### Why Go, standard library only

- Series conventions (single binary, four-platform distribution, signing /
  notarization).
- No official SDK exists and community wrappers are not adopted (the
  supply-chain rule). All three endpoints are plain REST; `net/http` plus
  `encoding/json` suffice.
- The Answers SSE is just `data:` lines read with `bufio.Scanner`. OpenAI SDK
  compatibility is not a reason to use an SDK.
- The official brave/brave-search-skills (MIT) is credited in README as the
  **specification reference**; no code is taken from it.

### Citations are streamed internally

Answers' `enable_citations` and `enable_research` work only with
`stream=true` (blocking mode returns 4xx). The user wants a finished answer
with sources, not incremental display, so **the engine always calls with
stream=true, reads the whole SSE stream, and assembles one response**. Parsing
the `<citation>` / `<usage>` / `<blindspots>` / `<progress>` tags is the
engine's job; the CLI and MCP receive assembled structures only. Incremental
display (`--stream`) is Phase 2.

### How it complements existing nlink-jp tools

| Tool | Role | Relation to brave-search |
|---|---|---|
| gem-search | agentic research on Vertex Grounding (three phases, report output) | coexist. "Investigate and write a report" is gem-search; "search and return material" is brave-search. research mode overlaps, but brave-search's runs server-side at Brave and needs no GCP |
| gem-agent / Claude Code | MCP clients | the main consumers, connecting brave-search as their web search tool |
| urlscan-lookup / rdns-lookup | investigation of a specific URL or domain | brave-search is **not a suspicious-URL tool**. It can search a name, but that only reads Brave's index |
| mcp-tactics | the MCP fleet playbook | one more server, so it must be updated (Phase 3) |

### Explicitly out of scope

- The Images / Videos / News / Local (POI) / Suggest / Spellcheck endpoints
  (News is a Phase 2 candidate; the rest wait for a demand)
- Rich data callback (`/web/rich`)
- Goggles and the `X-Loc-*` location headers (both Phase 2 candidates)
- Any disk cache of results, any save/export of results, live-response
  fixtures (ToS)
- HTTP/SSE MCP transport (stdio only, following data-toolbox-mcp's project
  ADR-0004)
- Multi-turn Answers conversations (the API accepts exactly one message)
- Own re-ranking, summarising or translation (the tool passes Brave's answer through)
- GUI

## 4. Development Plan

### Phase 1: Core

Four independently reviewable units, **preceded by one live measurement** (the
four open points of §5 and §7: key-to-plan mapping / `X-RateLimit-*` on
Answers / whether target pages are contacted / the real cost of research).

1. **Scaffold** (mirror of otx-lookup): Makefile / scripts / `internal/config`
   (sectioned TOML + `BRAVE_SEARCH_*`; keys match the parameter matrix) /
   `internal/app` (version / help / unimplemented commands exit 2) /
   `internal/mcp` skeleton (`get_usage` only; `notifications/cancelled` is
   received and ignored — upstream cannot be stopped) / this RFP and ADR-0001
   under `docs/{ja,en}`. `make build` with signing, `go test -race` green.
2. **Search endpoints**: `internal/brave` (`/web/search`, `/llm/context`
   client; `X-RateLimit-*` and `Api-Version` handling) / `internal/engine` /
   CLI `web` `context` / MCP `web_search` `llm_context`. Every path tested
   against httptest with synthetic fixtures.
3. **Answers endpoints**: the SSE reader and tag parser (`<citation>`
   `<usage>` `<blindspots>` `<progress>` `<answer>`) / CLI `answer` `research`
   (stderr progress) / MCP `answer` `research`. SSE fixtures synthesised after
   confirming the format against the live API.
4. **auth check + error contract + e2e**: structured errors for
   401/403/422/429 (`missing_api_key` / `unauthorized` / `plan_not_subscribed`
   / `invalid_arguments` / `rate_limited` with reset seconds in details /
   `upstream_error` / `timeout`). Live tests behind the `e2e` tag (skip without
   a key, **at most 20 requests in total**, bodies asserted but never saved).
   Meta-tests pinning `usage.md` to the code (every tool name, argument and
   error code must appear).

### Phase 2: Features

- News Search (`/news/search`)
- Goggles (inline rules only)
- `--stream` (incremental display for answer / research)
- Local usage ledger (monthly request counts and cost; no Search Results)
- Location headers (`X-Loc-*`)
- Exposing research's per-query token cap

### Phase 3: Release

- README.md / README.ja.md (user-facing information only; the ToS
  consequence — no cache — is stated in one line as the reason a feature is
  absent), CHANGELOG, AGENTS.md (dated live measurements in Gotchas)
- Release (signing, notarization, verify-release, four archives: linux
  amd64/arm64, darwin arm64, windows amd64), homebrew-tap
- util-series submodule integration, org profile, nlink-web-site card
- **mcp-tactics update** (both the body rows and the description trigger words)
- Feed knowledge back (SSE tag parsing, the ToS-driven absence of a cache, the
  reason for synthetic fixtures; and correct the "the spec has no cancel
  notification" error in mcp-server-design.md at the same time)
- `check-org.sh` all green

## 5. Required API Scopes / Permissions

- A Brave Search API **key** (`X-Subscription-Token`). No OAuth, no IAM.
- Plans required: **Search** (`/web/search`, `/llm/context`) and **Answers**
  (`/chat/completions`). Both subscribed.
- **Open (measured at the start of Phase 1)**: whether one key spans both
  plans or each plan issues its own key. The documentation does not say. If
  they are separate, add `answers_api_key` under `[api]` with a fallback to
  `api_key`. §1's "runs on a Brave API key alone" is to be read in this sense.
- `auth check` hits Search and Answers with one request each and reports the
  presence of each plan separately.

## 6. Series Placement

Series: **util-series**
Reason: gem-search (the web search CLI) and the MCP servers live here. It could
be read into cli-series ("interactive CLI clients for external services,
user-authenticated"), but brave-search is not interactive — it is a
pipe-friendly search primitive — and having an MCP face puts it on the same
shelf as data-toolbox-mcp / ask-gemini-mcp. Not cybersecurity-series: it is
not IR-specific and does not fit the `*-lookup` "one IoC → one attribute"
convention.

## 7. External Platform Constraints

Transcribed from the public documentation and the official skills as of
2026-09-12. **Phase 1 measures them and replaces this with dated Gotchas in
AGENTS.md.**

**Terms of Service (revision effective 2026-09-01)**:
- §3(b)(i) no storing, caching or database of Search Results beyond transient
  operational retention → no disk cache, no live-response fixtures
- §3(b)(xii) no redistribution, resale or sublicensing → no export feature
- §3(b)(xiii) no use to train, evaluate, benchmark or improve AI models →
  stated in README (the tool only feeds inference-time input)
- §3(b)(v) no circumventing rate limits, e.g. via multiple accounts
- "Search Results" is defined to include internet search results, Generated
  Results (Answers output) and Third-Party Content → the Answers text gets the
  same treatment
- §4 attribution is optional. Whether README says "Powered by Brave" is the
  user's call

**Pricing** (verify: the dashboard's figures are authoritative):
- Search: $5 per 1,000 requests; LLM Context is on the same plan; $5 free
  credit per month
- Answers: $4 per 1,000 searches + $5 per 1M tokens (in and out). At the API
  defaults one research call may search up to 20 queries × 4 iterations, so
  **a single call above $0.10 is ordinary** → tighter defaults (10 queries,
  2 iterations, 120 seconds; matches the parameter matrix; revisit after
  measurement)

**Rate limits**:
- One-second sliding window. Search 50 qps, Answers 2 qps (plan defaults)
- `X-RateLimit-Limit / Policy / Remaining / Reset` headers (documented for
  Search; **unverified for Answers**). A 429 is not a success and is not
  billed. Wait `Reset` seconds and retry with exponential backoff. No bulk
  mode, so pacing is simple

**Web Search**:
- `q` 1–400 characters, at most 50 words. `count` 1–20, `offset` 0–9 (so at
  most 200 results)
- `freshness` is `pd/pw/pm/py` or `YYYY-MM-DDtoYYYY-MM-DD`
- spellcheck defaults on → always show `query.altered`
- `result_filter` mixes in non-web sections (news / videos / faq / infobox /
  discussions / locations). Phase 1 pins it to `web`
- Each result carries many optional fields (schemas / product / recipe …) →
  compact shape by default

**LLM Context**:
- Accepted parameters: `q`, `country`, `search_lang`, `count` (1–50),
  `spellcheck`, `freshness`, `safesearch`, `maximum_number_of_tokens`
  (1024–32768, default 8192), `maximum_number_of_urls` (1–50),
  `maximum_number_of_snippets`, `context_threshold_mode`
- Response is `grounding.generic[]` + `sources`; its size scales with the
  token budget → `max_tokens` is the MCP cap

**Answers**:
- `POST /chat/completions`, `model: "brave"`, `messages` holds **exactly one**
  message
- Accepted parameters: `stream`, `country`, `language`, `safesearch`,
  `max_completion_tokens`, `enable_citations`, `enable_research`, `research_*`
- `enable_citations` / `enable_research` require `stream=true`. research and
  citations cannot be combined (research carries its citations inside
  `<answer>`)
- research ranges: queries 1–50, iterations 1–5, seconds 1–300, results per
  query 1–60, tokens per query 1024–16384
- Recommended timeouts: 30 s or more for single-search, 300 s or more for research
- Tags inside the SSE: `<citation>{start_index,end_index,number,url,favicon,snippet}`,
  `<usage>{X-Request-Requests, X-Request-Queries, X-Request-Tokens-In/Out, the
  per-component Cost fields, X-Request-Total-Cost}`; research adds `<queries>`
  `<analyzing>` `<thinking>` (debug, discarded), `<progress>`, `<blindspots>`,
  `<answer>`
- **Cancellation**: the MCP specification defines `notifications/cancelled`,
  but receivers MAY ignore it when the request "cannot be cancelled", and the
  vendored skeleton (otx-lookup `internal/mcp/server.go`) receives and
  ignores the notification. Whichever way a client stops waiting, **the
  upstream research runs to completion and is billed**. Keep the
  `max_seconds` default small and say so in usage.md

**Versioning**: `Api-Version: YYYY-MM-DD` header; unset means latest.
Incompatible changes are fenced by this header → **the client pins the date it
was implemented against** (overridable via `[api] api_version`)

**Whether Answers / LLM Context touch the target page live is unverified.**
The design reads as returning Brave's index and pre-extracted chunks, but it
matters for mcp-tactics' tier (third-party query vs target contact), so Phase 1
checks the official description and records it.

## Post-implementation corrections (2026-09-12)

Where live measurement contradicted this RFP. AGENTS.md Gotchas is the
canonical record from here on.

1. **One key per plan** (confirmed by the operator on the dashboard). Added
   `[api] answers_api_key` / `BRAVE_SEARCH_ANSWERS_API_KEY`, **required** for
   answer / research. §5's "fallback to `api_key`" is withdrawn — without it
   the tool refuses before sending (`missing_api_key`). The API nevertheless
   accepts either key on either endpoint (`/web/search` with the Answers key
   returned 200 under the Answers plan's rate policy): a key is not
   endpoint-locked, it selects which plan is billed and throttled. The tool
   keeps them separate.
2. **`Api-Version` defaults to empty (latest).** An arbitrary date
   (2026-09-12) was refused with 404 "product api version is not found".
   "Pin the implementation date" is withdrawn.
3. **A bad key is a 422, not a 401** (`error.code = SUBSCRIPTION_TOKEN_INVALID`),
   the same status as a validation failure (`VALIDATION`). Errors are mapped
   by upstream code first, status second; `auth check` relies on that
   distinction for its unbilled probe.
4. **One answer ≈ $0.054–0.058** (1 search + ~10,000 input tokens). §7's
   pricing note ("$4 per 1,000 searches + tokens") missed that the retrieved
   pages are billed as input tokens. Ten times a web search. Stated in
   usage.md and README.
5. **Citations are unreliable for non-English replies, and not
   deterministic** (one question, country constant, five runs: en → 29, 29;
   ja → 0, 0, 24). A non-English answer without citations carries a `note`,
   and an empty `citations` is never read as "no sources".
6. **The Answers endpoint returns no `X-RateLimit-*` headers** (the Search
   endpoints do). §2's "every command reports the budget left" holds for the
   Search commands only.
7. **A pay-as-you-go key's monthly window has limit 0** (`50;w=1,
   0;w=2592000`): 0 means uncapped, not exhausted; `ResetSeconds` skips such
   windows.
8. **Result shapes as built**: `llm_context` returns `chunks[]` (sources
   folded into each chunk); `answer`'s usage is folded into `meta`
   (`searches` / `tokens_in` / `tokens_out` / `cost_usd`). §2's tool table
   ("`grounding.generic[]` + `sources`", "`usage`") predates implementation.
9. **The query length limit (400 chars / 50 words) applies to web / context
   only**, per the matrix's scope column; an implementation that briefly
   applied it to answer / research questions was corrected.
10. **The stream tags have no escaping.** JSON-carrying tags (citation /
    usage / progress) are honoured only when their body is a JSON object;
    text-carrying tags (answer / blindspots / debug) are stripped as Brave's
    since they cannot be told apart. Recorded as a known limit in AGENTS.md.
11. **The research `<answer>` body is a JSON object** (`{"answer": "…"}`),
    not prose. The parser unwraps `answer`, reads `citations` / `blindspots`
    keys when present, and keeps any other key verbatim in `answer_extra`.
    Both live runs returned neither citations nor blind spots. `<progress>`
    arrives once per iteration (`number_of_*` keys and `elasped_seconds`).
    Capped at 2 queries / 1 iteration / 60 s, Brave ran 1 query in ~10 s for
    $0.063.

---

## Discussion Log

- **Opened 2026-09-12.** User request: a CLI + MCP using the Brave Web Search
  API and Answers API.
- Conflicts with the gem-search record (Brave rejected over ToS §3(b)), so the
  2026-09-01 ToS text was checked. Storage / redistribution / training bans
  stand, but there is no restriction on inference-time use and §4 attribution
  is optional. The reversal was decided on the use (primitives only) and the
  fact of an existing subscription. To be recorded in ADR-0001.
- Four user decisions: ① both Search and Answers plans subscribed ② name
  brave-search ③ Phase 1 covers Web + Answers + **LLM Context** (same plan, no
  extra cost) ④ research mode **from Phase 1**.
- Name alternatives: brave-lookup (cybersecurity naming, but the use is not
  IR), web-search (backend-neutral, but implies merging with gem-search) —
  rejected.
- Considered and not taken: using an SDK because of OpenAI compatibility
  (stdlib suffices; supply-chain rule), a disk cache like the lookup family
  (ToS), server-side spill (retired fleet-wide), an `--api-key` flag (lands in
  history).
- **rev 2 (same day)**: an independent review raised 16 findings, all
  adopted. The root cause of the four same-type findings (CLI / MCP / config
  argument mismatches) was keeping three tables → merged into one parameter
  matrix from which the other tables derive. Two high: (a) SSE fixtures
  captured from the live API amount to storing and redistributing Search
  Results → synthetic fixtures, (b) "the MCP spec has no cancel notification"
  was wrong (`notifications/cancelled` exists; receivers MAY ignore it and the
  in-house skeleton ignores it) → wording corrected; the same error in the
  knowledge base is corrected in Phase 3.
- Open, to be measured at the start of Phase 1: whether one key spans both
  plans / whether Answers returns `X-RateLimit-*` / whether Answers and LLM
  Context contact target pages live / the real cost of research.
