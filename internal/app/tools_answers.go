package app

import (
	"context"
	"encoding/json"

	"github.com/nlink-jp/brave-search/internal/engine"
	"github.com/nlink-jp/brave-search/internal/mcp"
)

var argLanguage = map[string]any{"type": "string", "description": "Reply language code such as en or ja (default from config, en)."}

type answerArgs struct {
	Question   string `json:"question"`
	Country    string `json:"country"`
	Language   string `json:"language"`
	Safesearch string `json:"safesearch"`
	MaxTokens  int    `json:"max_tokens"`
}

func answerTool(eng *engine.Engine) mcp.Tool {
	return mcp.Tool{
		Name: ToolAnswer,
		Description: "Brave Answers, single search: a grounded answer to one question with numbered citations " +
			"(url, snippet, span in the answer text). Billed per search plus tokens; `meta` carries Brave's own " +
			"cost report. Use this when one search should settle the question; use research when it will not. " +
			"Nothing is cached.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question":   map[string]any{"type": "string", "description": "The question, as a user would ask it. Exactly one message is sent; there is no conversation."},
				"country":    argCountry,
				"language":   argLanguage,
				"safesearch": argSafesearch,
				"max_tokens": map[string]any{"type": "integer", "description": "Reply token cap. Omit to let the API decide."},
			},
			"required": []string{"question"},
		},
		Handle: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a answerArgs
			if err := mcp.DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			return eng.Answer(ctx, engine.AnswerRequest{
				Question: a.Question, Country: a.Country, Language: a.Language, Safesearch: a.Safesearch, MaxTokens: a.MaxTokens,
			})
		},
	}
}

type researchArgs struct {
	Question      string `json:"question"`
	Country       string `json:"country"`
	Language      string `json:"language"`
	Safesearch    string `json:"safesearch"`
	MaxQueries    int    `json:"max_queries"`
	MaxIterations int    `json:"max_iterations"`
	MaxSeconds    int    `json:"max_seconds"`
}

func researchTool(eng *engine.Engine) mcp.Tool {
	return mcp.Tool{
		Name: ToolResearch,
		Description: "Brave Answers, research mode: Brave runs several searches over several iterations, reads the " +
			"pages, and returns a synthesised answer with citations plus the blind spots it could not cover. " +
			"THE EXPENSIVE TOOL: billed for every search it runs (up to max_queries × max_iterations) plus tokens, " +
			"and it cannot be stopped once dispatched — a cancelled call is still billed. Takes up to max_seconds " +
			"(default 120). Prefer answer or llm_context unless one search has proved insufficient. Nothing is cached.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question":       map[string]any{"type": "string", "description": "The question. Exactly one message is sent."},
				"country":        argCountry,
				"language":       argLanguage,
				"safesearch":     argSafesearch,
				"max_queries":    map[string]any{"type": "integer", "description": "Searches per iteration, 1-50 (default from config, 10 — the API's own default is 20)."},
				"max_iterations": map[string]any{"type": "integer", "description": "Iterations, 1-5 (default from config, 2 — the API's own default is 4)."},
				"max_seconds":    map[string]any{"type": "integer", "description": "Time budget, 1-300 (default from config, 120). The call cannot be cancelled, so keep this small."},
			},
			"required": []string{"question"},
		},
		Handle: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a researchArgs
			if err := mcp.DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			return eng.Research(ctx, engine.ResearchRequest{
				Question: a.Question, Country: a.Country, Language: a.Language, Safesearch: a.Safesearch,
				MaxQueries: a.MaxQueries, MaxIterations: a.MaxIterations, MaxSeconds: a.MaxSeconds,
			}, nil)
		},
	}
}
