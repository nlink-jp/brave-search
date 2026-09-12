package mcp

import _ "embed"

//go:embed usage.md
var usageDoc string

// UsageDoc returns the embedded manual — what get_usage answers with, and the
// canonical reference for this server's tools. The mcp-tactics skill documents
// no parameters by design, so this is what an agent reads before its first call.
func UsageDoc() string { return usageDoc }

// ToolGetUsage is the one tool every server in the fleet has.
const ToolGetUsage = "get_usage"

func usageTool() Tool {
	return Tool{
		Name:        ToolGetUsage,
		Description: "The full reference for this server: tools, arguments, result schema, cost notes and the error-recovery table. Call this first.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

// Instructions is what initialize hands the client. A model may act on it and
// tools/list alone without ever calling get_usage, so the facts that decide
// how to use this server correctly have to survive here.
const Instructions = `brave-search exposes the Brave Search API as four tools: web_search (ranked ` +
	`results with snippets), llm_context (pre-extracted page chunks sized for a model's context), ` +
	`answer (a grounded answer with citations, one search) and research (a multi-step, multi-search ` +
	`answer with citations and declared blind spots). Call get_usage first — it returns the full ` +
	`reference, the result schemas, the cost model and the error-recovery table. EVERY CALL COSTS ` +
	`MONEY: each result carries a meta block with the request's cost and the remaining rate budget, ` +
	`and research can spend a dollar-scale amount in one call, so keep its caps (max_queries, ` +
	`max_iterations, max_seconds) small and prefer answer or llm_context when one search will do. ` +
	`Nothing is cached: an identical call is a second request and a second charge. Results are ` +
	`Brave's and their sources' — cite them, do not store or redistribute them, and never use them ` +
	`to train or evaluate a model (Brave ToS §3(b)).`
