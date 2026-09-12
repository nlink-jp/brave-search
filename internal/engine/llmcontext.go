package engine

import (
	"context"

	"github.com/nlink-jp/brave-search/internal/brave"
	"github.com/nlink-jp/brave-search/internal/config"
)

// ContextRequest is an LLM Context call as the CLI and MCP express it. Zero
// values take the configured defaults.
type ContextRequest struct {
	Query      string
	Count      int
	Country    string
	SearchLang string
	Freshness  string
	Safesearch string
	MaxTokens  int
	MaxURLs    int
}

// ContextResult is the stable shape of an LLM Context call: one chunk group
// per URL, with the source metadata folded in so a reader has one list.
type ContextResult struct {
	Query    string  `json:"query"`
	URLs     int     `json:"urls"`
	Snippets int     `json:"snippets"`
	Chunks   []Chunk `json:"chunks"`
	Meta     Meta    `json:"meta"`
}

// Chunk is the extracted text of one URL plus what Brave knows about it.
type Chunk struct {
	URL      string   `json:"url"`
	Title    string   `json:"title,omitempty"`
	Hostname string   `json:"hostname,omitempty"`
	Age      string   `json:"age,omitempty"`
	Snippets []string `json:"snippets"`
}

// Context validates, fills defaults, and performs one LLM Context call.
func (e *Engine) Context(ctx context.Context, r ContextRequest) (*ContextResult, error) {
	p, err := e.contextParams(r)
	if err != nil {
		return nil, err
	}
	resp, meta, err := e.Client.LLMContext(ctx, p)
	if err != nil {
		return nil, err
	}
	out := &ContextResult{Query: p.Query, Chunks: make([]Chunk, 0, len(resp.Grounding.Generic)), Meta: searchMeta(meta)}
	for _, g := range resp.Grounding.Generic {
		c := Chunk{URL: g.URL, Title: g.Title, Snippets: g.Snippets}
		if src, ok := resp.Sources[g.URL]; ok {
			if c.Title == "" {
				c.Title = src.Title
			}
			c.Hostname = src.Hostname
			c.Age = pickAge(src.Age)
		}
		out.Chunks = append(out.Chunks, c)
		out.Snippets += len(g.Snippets)
	}
	out.URLs = len(out.Chunks)
	return out, nil
}

// pickAge chooses the ISO date form when Brave supplies several renderings,
// else the first.
func pickAge(ages []string) string {
	for _, a := range ages {
		if len(a) == 10 && a[4] == '-' && a[7] == '-' {
			return a
		}
	}
	if len(ages) > 0 {
		return ages[0]
	}
	return ""
}

func (e *Engine) contextParams(r ContextRequest) (brave.ContextParams, error) {
	s, c := e.Cfg.Search, e.Cfg.Context
	p := brave.ContextParams{
		Query:      r.Query,
		Count:      orDefaultInt(r.Count, s.Count),
		Country:    upstreamCountry(orDefault(r.Country, s.Country)),
		SearchLang: orDefault(r.SearchLang, s.SearchLang),
		Freshness:  r.Freshness,
		Safesearch: orDefault(r.Safesearch, s.Safesearch),
		MaxTokens:  orDefaultInt(r.MaxTokens, c.MaxTokens),
		MaxURLs:    orDefaultInt(r.MaxURLs, c.MaxURLs),
	}
	if err := validateQuery(p.Query); err != nil {
		return p, err
	}
	if err := validateRange("count", p.Count, 1, config.ContextCountMax); err != nil {
		return p, err
	}
	if err := validateCountry(p.Country); err != nil {
		return p, err
	}
	if err := validateLang("search_lang", p.SearchLang); err != nil {
		return p, err
	}
	if err := validateFreshness(p.Freshness); err != nil {
		return p, err
	}
	if err := validateSafesearch(p.Safesearch); err != nil {
		return p, err
	}
	if err := validateRange("max_tokens", p.MaxTokens, config.MaxTokensMin, config.MaxTokensMax); err != nil {
		return p, err
	}
	if err := validateRange("max_urls", p.MaxURLs, 1, config.MaxURLsMax); err != nil {
		return p, err
	}
	return p, nil
}
