// Package brave is the upstream client for the Brave Search API: Web Search
// (GET /web/search), LLM Context (GET /llm/context) and Answers
// (POST /chat/completions, always streamed). It uses net/http only.
//
// Every failure is a *Error with a stable code, so the CLI and the MCP layer
// surface one vocabulary. Every success carries a Meta with the rate-limit
// headers, because every call is billed and the caller must be able to see
// what is left.
package brave
