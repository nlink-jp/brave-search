package brave

import (
	"context"
	"net/http"
	"strings"
)

// Probe sends a deliberately invalid request to an endpoint and reports how
// Brave refused it. It exists for `auth check`: Brave has no "who am I"
// endpoint, and a real search would be billed, but the documentation says
// only successful responses count against the quota — so a request that is
// certain to fail validation (no query; no message) tells the key's status
// for free. A 401 means the key was rejected, a 403 that the plan is not
// subscribed, a 400/422 that the key was accepted and the request refused as
// intended. nil means Brave answered 2xx, which would be a surprise but is
// still "the key works".
//
// Whether Brave really checks the key before validating the request is a
// live measurement; the e2e suite pins it.
func (c *Client) Probe(ctx context.Context, endpoint string) (*Meta, error) {
	var req *http.Request
	var err error
	switch endpoint {
	case answersEndpoint:
		req, err = c.newRequest(ctx, http.MethodPost, endpoint, nil,
			strings.NewReader(`{"model":"brave","messages":[],"stream":false}`))
	default:
		req, err = c.newRequest(ctx, http.MethodGet, endpoint, nil, nil)
	}
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, transportError(endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	meta := &Meta{Status: resp.StatusCode, RateLimit: parseRateLimit(resp.Header)}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return meta, statusError(endpoint, resp, meta.RateLimit)
	}
	return meta, nil
}

// Endpoints, for callers that probe them.
const (
	EndpointWebSearch  = "/web/search"
	EndpointLLMContext = "/llm/context"
	EndpointAnswers    = answersEndpoint
)
