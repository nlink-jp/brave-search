# ADR-0001: Adopt the Brave Search API — reversing the rejection recorded in gem-search

> Status: Accepted
> Date: 2026-09-12

## Context

In 2026-04, when gem-search (util-series) chose its web search backend, the
Brave Search API — tried in the predecessor agentic-web-search — was
**rejected**: (1) the Terms of Service §3(b) restrictions (no storage,
redistribution or AI training of search results) looked aggressive, and
(2) paid registration was required. Vertex AI's Google Search Grounding was
chosen instead (see the gem-search RFP and architecture documents).

This project uses the Brave Search API. The same organisation reversing a
decision within five months deserves a written reason.

## Decision

**Adopt the Brave Search API (Web Search / LLM Context / Answers).**
The ToS imposes exactly two constraints on the design, both enforced in the
implementation.

1. **No search results on disk.** §3(b)(i) permits only "transient storage
   required for operation". The TTL'd file cache of the lookup family is not
   ported.
2. **No live responses in the repository.** "Search Results" is defined to
   include Generated Results (Answers text) and Third-Party Content. Test
   fixtures are synthesised after confirming the format against the live API;
   bodies, URLs and snippets are invented values.

## Rationale

- **The use differs.** gem-search feeds grounding results to its own LLM and
  recomposes them into a report, so storage and derivative-work questions
  follow it by design. brave-search returns search primitives and never
  recomposes, stores or redistributes them.
- **The ToS (revision effective 2026-09-01) was re-read.** It forbids storage
  and caching, derivative works, redistribution and resale, and using results
  to train, evaluate or improve AI models. **No clause restricts use as LLM
  input at inference time**, and LLM Context / Answers are sold for exactly
  that. §4's "Powered by Brave" mark applies only "if Customer elects to
  provide attribution".
- **The paid registration is done** (user decision). The psychological barrier
  is gone.
- A search entry point that needs no GCP is needed. gem-search presumes a GCP
  billing foundation.

## Alternatives considered

- **Vertex AI Web Grounding (reuse gem-search)** — too heavy as a search
  primitive and needs GCP. The uses differ, so the two coexist.
- **DuckDuckGo** — has no web search API, and its HTML endpoint is
  `Disallow: /` in robots.txt. Rejected in agentic-web-search ADR-001; not
  reconsidered.
- **Reusing the official brave/brave-search-skills (MIT)** — credited as the
  specification reference; no code is taken (the organisation does not adopt
  community-shaped or skill-shaped dependencies).

## Consequences

- No `cache` subcommand and no `[cache]` section.
- Every httptest / SSE fixture is synthetic. Live tests assert on response
  bodies without saving them.
- README states that search results must not be used to train or evaluate AI
  models (the tool only feeds inference-time input).
- The "Brave rejected" passage in gem-search's architecture document stays as a
  historical record; it and this ADR cross-reference each other.
