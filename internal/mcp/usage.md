# brave-search MCP server

The Brave Search API — Web Search, LLM Context and Answers — as MCP tools.
Nothing is cached, nothing is stored: every call is one billed request to
Brave, and every result is returned inline.

## Read this before the first call

- **Every call costs money.** The Search plan bills per request; the Answers
  plan bills per search *and* per token. Every result carries a `meta` block
  with what this request cost (`cost_usd`, exact for `answer` / `research` from
  Brave's own usage report, an estimate for the Search endpoints) and the rate
  budget left (`rate_limit`). Read it.
- **`research` is the expensive one.** One call may run up to `max_queries`
  searches per iteration for up to `max_iterations` iterations, then bill the
  tokens on top. The defaults here are tighter than the API's; raise them only
  when a single `answer` has proved insufficient.
- **A dispatched call cannot be stopped.** This server receives
  `notifications/cancelled` and ignores it, because the upstream search has
  already been dispatched and will complete and be billed regardless. Keep
  `max_seconds` small for that reason.
- **Nothing is cached**, by the Brave ToS (§3(b)(i)): an identical call is a
  second request and a second charge.
- **The results are not yours.** Cite the URLs; do not store, redistribute or
  use them to train or evaluate a model (§3(b)(xii), (xiii)).

## Which tool

| You want | Tool | Cost shape |
|---|---|---|
| URLs and snippets to read yourself | `web_search` | 1 request |
| Page text to reason over, sized to a token budget | `llm_context` | 1 request |
| A short answer with sources, one search | `answer` | 1 search + tokens |
| A thorough answer across many searches, with blind spots declared | `research` | up to `max_queries × max_iterations` searches + tokens |

## Tools

Every search tool shares four optional arguments: `country` (two-letter code
or ALL), `search_lang` (for web_search and llm_context) or `language` (for the
Answers tools), `freshness` (`pd` / `pw` / `pm` / `py` / `YYYY-MM-DDtoYYYY-MM-DD`)
and `safesearch` (`off` / `moderate` / `strict`). Unset arguments take the
operator's configured defaults (Brave's own: US, en, moderate). Every result
ends with a `meta` object:

```json
"meta": {"requests": 1, "cost_usd": 0.005, "cost_basis": "list_price_estimate",
         "rate_limit": {"limit": [1, 2000], "remaining": [0, 1999], "reset_seconds": [1, 86400]}}
```

`cost_basis` is `list_price_estimate` for the Search endpoints (Brave bills per
request at its published price) and `reported_by_brave` for the Answers tools
(Brave reports the exact figure). `rate_limit` lists one value per window —
usually the per-second burst limit first and the monthly quota second.

### `web_search`

Ranked web results for one query. One billed request.

| Argument | Type | Meaning |
|---|---|---|
| `query` | string, required | 1-400 characters, at most 50 words. Search operators (`site:`, `-term`, `"phrase"`) work. |
| `count` | integer | Results, 1-20 (default 10). |
| `offset` | integer | Page, 0-9. Walk further results with the same `count` while `more_results_available` is true. |
| `country`, `search_lang`, `freshness`, `safesearch` | string | As above. |
| `extra_snippets` | boolean | Also return up to five extra excerpts per result. |

Result: `query`, `altered` (present only when Brave spell-corrected the query —
the results then answer the altered form, so say so), `count`, `offset`,
`more_results_available`, and `results[]` of `{rank, title, url, description,
age, language, extra_snippets}`. The response is compact by design: the mixed
sections Brave can return (news, videos, infobox, discussions) are not
included, and the per-result structured data (schemas, ratings, products) is
dropped. The CLI's `--full` exposes it; this tool does not, to keep the
response bounded.

### `llm_context`

Page text pre-extracted from the top results and sized to a token budget: the
endpoint built for grounding a model's answer. One billed request.

| Argument | Type | Meaning |
|---|---|---|
| `query` | string, required | The query or question, 1-400 characters, at most 50 words. |
| `count` | integer | Results to analyse, 1-50 (default 10). |
| `country`, `search_lang`, `freshness`, `safesearch` | string | As above. |
| `max_tokens` | integer | Token budget for the returned context, 1024-32768 (default 8192). **This is the response-size cap** — start small. |
| `max_urls` | integer | URLs the context may draw from, 1-50 (default 20). |

Result: `query`, `urls`, `snippets`, and `chunks[]` of `{url, title, hostname,
age, snippets[]}` — one entry per URL, snippets in Brave's relevance order.
Brave's guidance: 5 results / 2048 tokens for a simple factual question, the
defaults for standard research, 50 / 16384 for a complex multi-source
question.

### `answer`

A grounded answer to one question, from one search, with citations. Billed
per search plus tokens; `meta` carries Brave's exact cost report.

| Argument | Type | Meaning |
|---|---|---|
| `question` | string, required | The question. Exactly one message is sent; there is no conversation. |
| `country`, `language`, `safesearch` | string | As above (`language` is the reply language). |
| `max_tokens` | integer | Reply token cap. Omit to let the API decide. |

Result: `question`, `mode: "answer"`, `answer` (the text), `citations[]` of
`{number, url, snippet, start_index, end_index}` (indexes into `answer`), and
`meta` with `searches`, `tokens_in`, `tokens_out` and `cost_usd` as Brave
reported them (`cost_basis: reported_by_brave`; `not_reported_by_brave` if the
stream carried no usage report, in which case the cost is unknown, not zero).

### `research`

Research mode: Brave runs several searches over several iterations, reads the
pages, and synthesises an answer with citations and declared blind spots.

**This is the expensive tool.** It is billed for every search it runs — up to
`max_queries × max_iterations` — plus tokens, and it cannot be stopped once
dispatched: a call the client abandons is still completed and billed. The
defaults here are tighter than the API's (10 queries / 2 iterations / 120 s
against 20 / 4 / 180). Prefer `answer` or `llm_context` unless one search has
proved insufficient.

| Argument | Type | Meaning |
|---|---|---|
| `question` | string, required | The question. |
| `country`, `language`, `safesearch` | string | As above. |
| `max_queries` | integer | Searches per iteration, 1-50 (default 10). |
| `max_iterations` | integer | Iterations, 1-5 (default 2). |
| `max_seconds` | integer | Time budget, 1-300 (default 120). The HTTP deadline is this plus 30 s. |

Result: as `answer`, with `mode: "research"`, plus `blindspots` (what Brave
says it could not cover — read it before trusting the answer) and
`progress[]` (Brave's iteration reports, for provenance).

### `get_usage`

Returns this manual.

## Errors

Tool errors come back as `isError: true` with a JSON body
`{code, message, details?}`. Branch on `code`.

| Code | Meaning | What to do |
|---|---|---|
| `invalid_arguments` | An argument is missing, misspelled, or out of range. | Fix the call. Ranges are in this manual; nothing was sent upstream. |
| `missing_api_key` | No API key is configured. | The operator sets `[api] api_key` in the config file or `BRAVE_SEARCH_API_KEY`. |
| `unauthorized` | Brave rejected the key (401). | The operator checks the key with `brave-search auth check`. Do not retry. |
| `plan_not_subscribed` | The key is valid but the endpoint's plan is not active (403). | Tell the operator which plan (Search or Answers) the tool needs. Do not retry. |
| `rate_limited` | Too many requests (429). `details.reset_seconds` says how long to wait. | Wait that long, then retry once. Do not loop. |
| `upstream_error` | Brave answered 5xx or something unexpected. | Retry once after a few seconds; then report it. |
| `timeout` | The request exceeded its deadline. | For `research`, lower `max_seconds` (the deadline is `max_seconds + 30 s`); otherwise retry once. |
| `network_error` | The request never completed. | Report it; the operator checks connectivity. |
| `decode_error` | Brave's response was not the JSON expected. | Report it with the message; the API may have changed. |
