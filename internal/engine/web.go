package engine

import (
	"context"
	"encoding/json"

	"github.com/nlink-jp/brave-search/internal/brave"
	"github.com/nlink-jp/brave-search/internal/config"
)

// WebRequest is a web search as the CLI and MCP express it. Zero values take
// the configured defaults.
type WebRequest struct {
	Query         string
	Count         int
	Offset        int
	Country       string
	SearchLang    string
	Freshness     string
	Safesearch    string
	ExtraSnippets bool
	Full          bool // pass every upstream field through (CLI only)
}

// WebResult is the compact, stable shape of a web search.
type WebResult struct {
	Query                string            `json:"query"`
	Altered              string            `json:"altered,omitempty"`
	Count                int               `json:"count"`
	Offset               int               `json:"offset"`
	MoreResultsAvailable bool              `json:"more_results_available"`
	Results              []Hit             `json:"results"`
	Full                 []json.RawMessage `json:"full,omitempty"`
	Meta                 Meta              `json:"meta"`
}

// Hit is one ranked result, compact by design: title, URL, snippet, age and
// language are what a reader or an agent acts on. Everything else stays
// behind --full.
type Hit struct {
	Rank          int      `json:"rank"`
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	Description   string   `json:"description"`
	Age           string   `json:"age,omitempty"`
	Language      string   `json:"language,omitempty"`
	ExtraSnippets []string `json:"extra_snippets,omitempty"`
}

// Web validates, fills defaults, and performs one web search.
func (e *Engine) Web(ctx context.Context, r WebRequest) (*WebResult, error) {
	p, err := e.webParams(r)
	if err != nil {
		return nil, err
	}
	resp, meta, err := e.Client.WebSearch(ctx, p)
	if err != nil {
		return nil, err
	}
	out := &WebResult{
		Query:                resp.Query.Original,
		Count:                len(resp.Web.Results),
		Offset:               p.Offset,
		MoreResultsAvailable: resp.Query.MoreResultsAvailable,
		Results:              make([]Hit, 0, len(resp.Web.Results)),
		Meta:                 searchMeta(meta),
	}
	if out.Query == "" {
		out.Query = p.Query
	}
	if resp.Query.Altered != "" && resp.Query.Altered != resp.Query.Original {
		out.Altered = resp.Query.Altered
	}
	for i, h := range resp.Web.Results {
		out.Results = append(out.Results, Hit{
			Rank: p.Offset*p.Count + i + 1, Title: h.Title, URL: h.URL, Description: h.Description,
			Age: h.Age, Language: h.Language, ExtraSnippets: h.ExtraSnippets,
		})
		if r.Full {
			out.Full = append(out.Full, h.Raw)
		}
	}
	return out, nil
}

func (e *Engine) webParams(r WebRequest) (brave.WebParams, error) {
	s := e.Cfg.Search
	p := brave.WebParams{
		Query:         r.Query,
		Count:         orDefaultInt(r.Count, s.Count),
		Offset:        r.Offset,
		Country:       upstreamCountry(orDefault(r.Country, s.Country)),
		SearchLang:    orDefault(r.SearchLang, s.SearchLang),
		Freshness:     r.Freshness,
		Safesearch:    orDefault(r.Safesearch, s.Safesearch),
		ExtraSnippets: r.ExtraSnippets,
	}
	if err := validateQuery(p.Query); err != nil {
		return p, err
	}
	if err := validateRange("count", p.Count, 1, config.WebCountMax); err != nil {
		return p, err
	}
	if err := validateRange("offset", p.Offset, 0, config.OffsetMax); err != nil {
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
	return p, nil
}
