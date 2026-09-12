package brave

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

// WebParams are the Web Search query parameters this tool exposes. Ranges
// are validated by the engine before a request is built.
type WebParams struct {
	Query         string
	Count         int
	Offset        int // page, 0-9; sent even when 0? no — 0 is the default and omitted
	Country       string
	SearchLang    string
	Freshness     string
	Safesearch    string
	ExtraSnippets bool
}

// WebResponse is the subset of WebSearchApiResponse this tool reads, plus the
// raw body for pass-through.
type WebResponse struct {
	Type  string    `json:"type"`
	Query QueryInfo `json:"query"`
	Web   struct {
		Results []WebResult `json:"results"`
	} `json:"web"`
	Raw json.RawMessage `json:"-"`
}

// QueryInfo reports what Brave did with the query. Altered is set when
// spellcheck rewrote it, and must always be shown: an answer to a different
// question is not an answer.
type QueryInfo struct {
	Original             string `json:"original"`
	Altered              string `json:"altered,omitempty"`
	Cleaned              string `json:"cleaned,omitempty"`
	SpellcheckOff        bool   `json:"spellcheck_off,omitempty"`
	MoreResultsAvailable bool   `json:"more_results_available"`
}

// WebResult is one ranked result. Raw keeps every upstream field for --full.
type WebResult struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	Description   string   `json:"description"`
	Age           string   `json:"age,omitempty"`
	PageAge       string   `json:"page_age,omitempty"`
	Language      string   `json:"language,omitempty"`
	ExtraSnippets []string `json:"extra_snippets,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps the raw object beside the decoded fields.
func (r *WebResult) UnmarshalJSON(b []byte) error {
	type plain WebResult
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*r = WebResult(p)
	r.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// WebSearch performs GET /web/search.
//
// result_filter is pinned to "web" and text_decorations to false: this tool
// returns web results as plain text, and the mixed sections (news, videos,
// infobox, ...) each deserve their own tool if they are ever wanted.
func (c *Client) WebSearch(ctx context.Context, p WebParams) (*WebResponse, *Meta, error) {
	q := url.Values{}
	q.Set("q", p.Query)
	q.Set("result_filter", "web")
	q.Set("text_decorations", "false")
	setInt(q, "count", p.Count)
	if p.Offset > 0 {
		q.Set("offset", strconv.Itoa(p.Offset))
	}
	setIf(q, "country", p.Country)
	setIf(q, "search_lang", p.SearchLang)
	setIf(q, "freshness", p.Freshness)
	setIf(q, "safesearch", p.Safesearch)
	if p.ExtraSnippets {
		q.Set("extra_snippets", "true")
	}

	var out WebResponse
	raw, meta, err := c.getJSON(ctx, "/web/search", q, &out)
	if err != nil {
		return nil, meta, err
	}
	out.Raw = raw
	return &out, meta, nil
}
