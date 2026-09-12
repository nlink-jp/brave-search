package brave

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Doer is the HTTP surface the client depends on, so tests inject a stub.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client talks to the Brave Search API.
type Client struct {
	BaseURL    string
	APIKey     string
	APIVersion string // Api-Version header; "" sends none
	UserAgent  string
	HTTP       Doer

	// AnswersAPIKey, when set, is sent to /chat/completions instead of
	// APIKey. Whether Brave issues one key per plan or one key for all is
	// unverified; this keeps both cases working.
	AnswersAPIKey string
}

// New builds a client with a plain net/http transport. timeout bounds each
// exchange; research callers pass a longer one.
func New(baseURL, apiKey, apiVersion string, timeout time.Duration, userAgent string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		APIVersion: apiVersion,
		UserAgent:  userAgent,
		HTTP:       &http.Client{Timeout: timeout},
	}
}

// WithTimeout returns a copy whose HTTP client has a different deadline. Used
// for research, whose deadline is derived from its own time budget.
func (c *Client) WithTimeout(d time.Duration) *Client {
	clone := *c
	clone.HTTP = &http.Client{Timeout: d}
	return &clone
}

// HasKey reports whether the client is authenticated for the Search endpoints.
func (c *Client) HasKey() bool { return c.APIKey != "" }

// keyFor returns the key to send to an endpoint.
func (c *Client) keyFor(endpoint string) string {
	if endpoint == answersEndpoint && c.AnswersAPIKey != "" {
		return c.AnswersAPIKey
	}
	return c.APIKey
}

// maxBody caps how much of a response is read. A context response at the
// largest token budget is a few hundred KB; anything past this is a
// malfunction, not data.
const maxBody = 16 << 20

// Meta is what every successful call reports beside its payload.
type Meta struct {
	Status    int        `json:"status"`
	RateLimit *RateLimit `json:"rate_limit,omitempty"`
}

// RateLimit is the parsed X-RateLimit-* header set. Brave reports one value
// per window — typically a per-second burst limit and a monthly quota — as
// comma-separated lists, e.g. "1, 15000" with policy "1;w=1, 15000;w=2592000".
type RateLimit struct {
	Limit     []int  `json:"limit,omitempty"`
	Remaining []int  `json:"remaining,omitempty"`
	Reset     []int  `json:"reset_seconds,omitempty"`
	Policy    string `json:"policy,omitempty"`
}

// ResetSeconds returns how long to wait before the exhausted window resets:
// the reset of the first window whose remaining count is zero, else the first
// reset value. ok is false when no reset header was present.
func (r *RateLimit) ResetSeconds() (int, bool) {
	if r == nil || len(r.Reset) == 0 {
		return 0, false
	}
	for i, rem := range r.Remaining {
		if rem <= 0 && i < len(r.Reset) {
			return r.Reset[i], true
		}
	}
	return r.Reset[0], true
}

func parseRateLimit(h http.Header) *RateLimit {
	rl := &RateLimit{
		Limit:     intList(h.Get("X-RateLimit-Limit")),
		Remaining: intList(h.Get("X-RateLimit-Remaining")),
		Reset:     intList(h.Get("X-RateLimit-Reset")),
		Policy:    h.Get("X-RateLimit-Policy"),
	}
	if len(rl.Limit) == 0 && len(rl.Remaining) == 0 && len(rl.Reset) == 0 && rl.Policy == "" {
		return nil
	}
	return rl
}

func intList(s string) []int {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// newRequest builds a request with the headers every endpoint needs. It
// refuses to build one without a key: nothing is sent that Brave would only
// reject.
func (c *Client) newRequest(ctx context.Context, method, endpoint string, query url.Values, body io.Reader) (*http.Request, error) {
	key := c.keyFor(endpoint)
	if key == "" {
		return nil, &Error{Code: CodeMissingAPIKey,
			Message: "no Brave API key is configured; set [api] api_key in the config file or BRAVE_SEARCH_API_KEY",
			Details: map[string]any{"endpoint": endpoint}}
	}
	u := c.BaseURL + endpoint
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, &Error{Code: CodeNetwork, Message: fmt.Sprintf("build request: %v", err)}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", key)
	if c.APIVersion != "" {
		req.Header.Set("Api-Version", c.APIVersion)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// getJSON performs a GET and decodes a 2xx body into out, returning the
// raw body alongside so callers can pass fields through untouched.
func (c *Client) getJSON(ctx context.Context, endpoint string, query url.Values, out any) (json.RawMessage, *Meta, error) {
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, query, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, transportError(endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	meta := &Meta{Status: resp.StatusCode, RateLimit: parseRateLimit(resp.Header)}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, meta, statusError(endpoint, resp, meta.RateLimit)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, meta, transportError(endpoint, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, meta, decodeError(endpoint, err)
	}
	return raw, meta, nil
}

func setIf(q url.Values, key, val string) {
	if val != "" {
		q.Set(key, val)
	}
}

func setInt(q url.Values, key string, val int) {
	if val > 0 {
		q.Set(key, strconv.Itoa(val))
	}
}
