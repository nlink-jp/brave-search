package brave

import (
	"context"
	"encoding/json"
	"net/url"
)

// ContextParams are the LLM Context query parameters this tool exposes.
type ContextParams struct {
	Query      string
	Count      int
	Country    string
	SearchLang string
	Freshness  string
	Safesearch string
	MaxTokens  int
	MaxURLs    int
}

// ContextResponse is the LLM Context payload: grounding chunks plus a
// per-URL source index.
type ContextResponse struct {
	Grounding struct {
		Generic []ContextChunk `json:"generic"`
	} `json:"grounding"`
	Sources map[string]ContextSource `json:"sources"`
	Raw     json.RawMessage          `json:"-"`
}

// ContextChunk is the extracted text of one URL.
type ContextChunk struct {
	URL      string   `json:"url"`
	Title    string   `json:"title"`
	Snippets []string `json:"snippets"`
}

// ContextSource is the metadata Brave attaches to a URL. Age is documented as
// a list of rendered forms; Strings also accepts a single string in case the
// shape varies.
type ContextSource struct {
	Title       string  `json:"title,omitempty"`
	Hostname    string  `json:"hostname,omitempty"`
	Age         Strings `json:"age,omitempty"`
	Description string  `json:"description,omitempty"`
	SiteName    string  `json:"site_name,omitempty"`
}

// Strings decodes from either a JSON string or a JSON array of strings.
type Strings []string

// UnmarshalJSON accepts "x" and ["x", "y"].
func (s *Strings) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var one string
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		*s = Strings{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

// LLMContext performs GET /llm/context.
func (c *Client) LLMContext(ctx context.Context, p ContextParams) (*ContextResponse, *Meta, error) {
	q := url.Values{}
	q.Set("q", p.Query)
	setInt(q, "count", p.Count)
	setIf(q, "country", p.Country)
	setIf(q, "search_lang", p.SearchLang)
	setIf(q, "freshness", p.Freshness)
	setIf(q, "safesearch", p.Safesearch)
	setInt(q, "maximum_number_of_tokens", p.MaxTokens)
	setInt(q, "maximum_number_of_urls", p.MaxURLs)

	var out ContextResponse
	raw, meta, err := c.getJSON(ctx, "/llm/context", q, &out)
	if err != nil {
		return nil, meta, err
	}
	out.Raw = raw
	return &out, meta, nil
}
