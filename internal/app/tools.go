package app

import (
	"context"
	"encoding/json"

	"github.com/nlink-jp/brave-search/internal/config"
	"github.com/nlink-jp/brave-search/internal/engine"
	"github.com/nlink-jp/brave-search/internal/mcp"
)

// Tool names. The embedded manual references them and a meta-test pins the
// two together.
const (
	ToolWebSearch  = "web_search"
	ToolLLMContext = "llm_context"
	ToolAnswer     = "answer"
	ToolResearch   = "research"
)

// tools builds the tool set the MCP server exposes over one engine.
func tools(cfg *config.Config, version string) []mcp.Tool {
	eng := engine.New(cfg, newClient(cfg, version))
	return []mcp.Tool{webSearchTool(eng), llmContextTool(eng), answerTool(eng), researchTool(eng)}
}

// Shared argument descriptions, so the two search tools describe the same
// parameter the same way.
var (
	argCountry    = map[string]any{"type": "string", "description": "Two-letter country code or ALL (default from config, US)."}
	argSearchLang = map[string]any{"type": "string", "description": "Search language code such as en or ja (default from config, en)."}
	argFreshness  = map[string]any{"type": "string", "description": "pd (24h), pw (7d), pm (31d), py (1y) or YYYY-MM-DDtoYYYY-MM-DD."}
	argSafesearch = map[string]any{"type": "string", "enum": []string{"off", "moderate", "strict"}, "description": "Content filter (default from config, moderate)."}
)

type webSearchArgs struct {
	Query         string `json:"query"`
	Count         int    `json:"count"`
	Offset        int    `json:"offset"`
	Country       string `json:"country"`
	SearchLang    string `json:"search_lang"`
	Freshness     string `json:"freshness"`
	Safesearch    string `json:"safesearch"`
	ExtraSnippets bool   `json:"extra_snippets"`
}

func webSearchTool(eng *engine.Engine) mcp.Tool {
	return mcp.Tool{
		Name: ToolWebSearch,
		Description: "Brave Web Search: ranked results with title, URL, snippet, age and language for one query. " +
			"One billed request. Compact by design — the mixed sections (news, videos, infobox) are not returned. " +
			"If `altered` is present, Brave spell-corrected the query and the results answer the altered form. " +
			"Read `meta` for what the call cost and the rate budget left; nothing is cached.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":          map[string]any{"type": "string", "description": "The search query, 1-400 characters and at most 50 words. Search operators (site:, -term, \"phrase\") work."},
				"count":          map[string]any{"type": "integer", "description": "Results to return, 1-20 (default from config, 10)."},
				"offset":         map[string]any{"type": "integer", "description": "Page, 0-9. Use with the same count to walk further results when more_results_available is true."},
				"country":        argCountry,
				"search_lang":    argSearchLang,
				"freshness":      argFreshness,
				"safesearch":     argSafesearch,
				"extra_snippets": map[string]any{"type": "boolean", "description": "Also return up to five extra excerpts per result. Larger responses; useful when the description alone is too thin."},
			},
			"required": []string{"query"},
		},
		Handle: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a webSearchArgs
			if err := mcp.DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			return eng.Web(ctx, engine.WebRequest{
				Query: a.Query, Count: a.Count, Offset: a.Offset, Country: a.Country, SearchLang: a.SearchLang,
				Freshness: a.Freshness, Safesearch: a.Safesearch, ExtraSnippets: a.ExtraSnippets,
			})
		},
	}
}

type llmContextArgs struct {
	Query      string `json:"query"`
	Count      int    `json:"count"`
	Country    string `json:"country"`
	SearchLang string `json:"search_lang"`
	Freshness  string `json:"freshness"`
	Safesearch string `json:"safesearch"`
	MaxTokens  int    `json:"max_tokens"`
	MaxURLs    int    `json:"max_urls"`
}

func llmContextTool(eng *engine.Engine) mcp.Tool {
	return mcp.Tool{
		Name: ToolLLMContext,
		Description: "Brave LLM Context: page text pre-extracted from the top results and sized to a token budget, " +
			"grouped per URL with title, hostname and date — the endpoint built for grounding a model's answer. " +
			"One billed request. `max_tokens` bounds the whole response; start small (2048 for a factual question) " +
			"and raise it only when the answer needs more. Read `meta` for cost and rate budget; nothing is cached.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":       map[string]any{"type": "string", "description": "The query or question, 1-400 characters and at most 50 words."},
				"count":       map[string]any{"type": "integer", "description": "Results to analyse, 1-50 (default from config, 10)."},
				"country":     argCountry,
				"search_lang": argSearchLang,
				"freshness":   argFreshness,
				"safesearch":  argSafesearch,
				"max_tokens":  map[string]any{"type": "integer", "description": "Token budget for the returned context, 1024-32768 (default from config, 8192). This is the response-size cap."},
				"max_urls":    map[string]any{"type": "integer", "description": "URLs the context may draw from, 1-50 (default from config, 20)."},
			},
			"required": []string{"query"},
		},
		Handle: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a llmContextArgs
			if err := mcp.DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			return eng.Context(ctx, engine.ContextRequest{
				Query: a.Query, Count: a.Count, Country: a.Country, SearchLang: a.SearchLang,
				Freshness: a.Freshness, Safesearch: a.Safesearch, MaxTokens: a.MaxTokens, MaxURLs: a.MaxURLs,
			})
		},
	}
}
