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
| `timeout` | The request exceeded its deadline. | For `research`, lower `max_seconds`; otherwise retry once. |
| `network_error` | The request never completed. | Report it; the operator checks connectivity. |
| `decode_error` | Brave's response was not the JSON expected. | Report it with the message; the API may have changed. |
