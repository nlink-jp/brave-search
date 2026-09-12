// Package mcp is a dependency-free stdio JSON-RPC 2.0 MCP server exposing
// web_search, llm_context, answer, research and get_usage.
//
// get_usage is canonical: the mcp-tactics skill deliberately documents no
// parameters, so the embedded manual is what an agent reads before its first
// call. Tool errors are structured JSON ({code, message, details}).
//
// The MCP specification defines notifications/cancelled, and this server
// receives it and ignores it: a receiver MAY do so when a request cannot be
// cancelled, and an upstream search that has been dispatched cannot. A
// closing stdin is the shutdown signal.
package mcp
