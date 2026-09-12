# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Live measurements (2026-09-12) folded in: Brave's error code is read before
  the HTTP status (a bad key is a 422 `SUBSCRIPTION_TOKEN_INVALID`); a
  0-limit rate window is uncapped, not exhausted; a stream without `[DONE]`
  is an incomplete answer; citations proved unreliable for non-English
  replies, so a non-English answer without citations carries a `note`.
- `--` ends flag parsing so a query word starting with `-` (the exclusion
  operator) can be passed; `-h` on a subcommand prints usage and exits 0;
  `research --timeout` overrides the derived deadline; the 400-character /
  50-word limit applies to web and context only.
- `auth check`: per-plan key standing (valid / rejected / not subscribed /
  absent / unreachable) via unbilled invalid-request probes, plus the config
  file actually read and the paths searched.
- Live e2e suite (`make e2e`, `e2e` build tag) that measures the open
  questions and skips cleanly without a key; `scripts/e2e.sh` for the built
  binary's exit codes, stdout/stderr split and MCP stdio session.
- `answer` / `answer` and `research` / `research`: Brave Answers, always
  streamed and assembled into one result — answer text, citations, Brave's
  declared blind spots (research), and `meta` with Brave's own cost report
  (searches, tokens, USD). research defaults are tighter than the API's;
  its progress is printed to stderr while it runs; its HTTP deadline derives
  from `max_seconds`.
- `[api] answers_api_key` / `BRAVE_SEARCH_ANSWERS_API_KEY`: Brave issues one
  key per plan, so the Answers endpoint has its own required key and the
  Search key is never sent to it.
- `web` / `web_search`: Brave Web Search with count, offset, country,
  search language, freshness, safesearch and extra snippets; compact result
  shape with `--full` pass-through on the CLI; every result carries `meta`
  with the estimated cost and the rate budget left.
- `context` / `llm_context`: Brave LLM Context with token and URL budgets,
  source metadata folded into each chunk.
- Local validation of every documented range before a request is spent;
  structured errors (`invalid_arguments`, `missing_api_key`, `unauthorized`,
  `plan_not_subscribed`, `rate_limited` with reset seconds, `upstream_error`,
  `timeout`, `network_error`, `decode_error`).
- Scaffold: config resolution (sectioned TOML under `[api]` / `[search]` /
  `[context]` / `[answers]`, `BRAVE_SEARCH_*` environment variables, strict
  keys, local range validation), the CLI shell with the version and help
  contracts, and the stdio MCP server skeleton exposing `get_usage`.
