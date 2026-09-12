package brave

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// Error codes. They are the contract the MCP layer surfaces as
// {code, message, details}, so an agent can branch on them without parsing
// prose. invalid_arguments is shared with the MCP layer's own validation so
// a caller sees one code whether the check happened locally or upstream.
const (
	CodeInvalidArguments  = "invalid_arguments"   // rejected locally, or 4xx validation upstream
	CodeMissingAPIKey     = "missing_api_key"     // no key configured; nothing was sent
	CodeUnauthorized      = "unauthorized"        // 401: the key was rejected
	CodePlanNotSubscribed = "plan_not_subscribed" // 403: the key is valid but not for this endpoint's plan
	CodeRateLimited       = "rate_limited"        // 429: details.reset_seconds says how long to wait
	CodeUpstream          = "upstream_error"      // 5xx or an unexpected status
	CodeTimeout           = "timeout"             // the deadline passed
	CodeNetwork           = "network_error"       // the request never completed
	CodeDecode            = "decode_error"        // the body was not the JSON expected
)

// Codes lists every code this package can produce, for the manual meta-test.
func Codes() []string {
	return []string{CodeInvalidArguments, CodeMissingAPIKey, CodeUnauthorized, CodePlanNotSubscribed,
		CodeRateLimited, CodeUpstream, CodeTimeout, CodeNetwork, CodeDecode}
}

// Error is a structured failure.
type Error struct {
	Code    string
	Message string
	Status  int
	Details map[string]any
}

func (e *Error) Error() string { return e.Message }

// ErrorCode implements the MCP layer's Coded interface.
func (e *Error) ErrorCode() string { return e.Code }

// ErrorDetails implements the MCP layer's Coded interface.
func (e *Error) ErrorDetails() map[string]any { return e.Details }

// Code returns the error's code, or "" when err is not an *Error.
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Argf builds an invalid_arguments error. Used by local validation so the
// caller never spends a request on a value the documentation already rules
// out.
func Argf(format string, args ...any) error {
	return &Error{Code: CodeInvalidArguments, Message: fmt.Sprintf(format, args...)}
}

// errorBody is the shape Brave uses for failures:
// {"type":"ErrorResponse","error":{"id","status","code","detail","meta"},"time"}.
// Every field is optional in practice, so decoding is tolerant.
type errorBody struct {
	Type  string `json:"type"`
	Error struct {
		ID     string          `json:"id"`
		Status int             `json:"status"`
		Code   string          `json:"code"`
		Detail string          `json:"detail"`
		Meta   json.RawMessage `json:"meta"`
	} `json:"error"`
}

// statusError maps a non-2xx response onto an *Error. The body is read (up
// to a cap) so the upstream code and detail reach the caller in Details.
func statusError(endpoint string, resp *http.Response, rl *RateLimit) *Error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var eb errorBody
	_ = json.Unmarshal(body, &eb)

	details := map[string]any{"status": resp.StatusCode, "endpoint": endpoint}
	if eb.Error.Code != "" {
		details["upstream_code"] = eb.Error.Code
	}
	if eb.Error.Detail != "" {
		details["upstream_detail"] = eb.Error.Detail
	}
	if len(eb.Error.Meta) > 0 && string(eb.Error.Meta) != "null" {
		details["upstream_meta"] = json.RawMessage(eb.Error.Meta)
	}
	summary := eb.Error.Detail
	if summary == "" {
		summary = strings.TrimSpace(string(body))
		if len(summary) > 200 {
			summary = summary[:200] + "…"
		}
	}
	if summary == "" {
		summary = http.StatusText(resp.StatusCode)
	}

	e := &Error{Status: resp.StatusCode, Details: details}
	// The upstream code decides before the status does. Measured 2026-09-12:
	// a bad key is answered 422 with code SUBSCRIPTION_TOKEN_INVALID, the
	// same status as a validation failure (code VALIDATION), and a missing
	// token header is a VALIDATION error naming the header. The status alone
	// cannot tell "your key is wrong" from "your request is wrong".
	switch strings.ToUpper(eb.Error.Code) {
	case "SUBSCRIPTION_TOKEN_INVALID":
		e.Code, e.Message = CodeUnauthorized, "Brave rejected the API key: "+summary
		return e
	case "VALIDATION":
		if isMissingTokenHeader(eb.Error.Meta) {
			e.Code, e.Message = CodeUnauthorized, "Brave saw no API key on the request: "+summary
			return e
		}
		e.Code, e.Message = CodeInvalidArguments, "Brave rejected the request parameters: "+summary
		return e
	case "RATE_LIMITED":
		e.Code, e.Message = CodeRateLimited, "Brave rate limit exceeded: "+summary
		addReset(details, rl)
		return e
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		e.Code, e.Message = CodeUnauthorized, "Brave rejected the API key: "+summary
	case resp.StatusCode == http.StatusForbidden:
		e.Code, e.Message = CodePlanNotSubscribed, "Brave refused this endpoint for the configured key (is its plan subscribed?): "+summary
	case resp.StatusCode == http.StatusTooManyRequests:
		e.Code, e.Message = CodeRateLimited, "Brave rate limit exceeded: "+summary
		addReset(details, rl)
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity:
		e.Code, e.Message = CodeInvalidArguments, "Brave rejected the request parameters: "+summary
	case resp.StatusCode >= 500:
		e.Code, e.Message = CodeUpstream, fmt.Sprintf("Brave answered %d: %s", resp.StatusCode, summary)
	default:
		e.Code, e.Message = CodeUpstream, fmt.Sprintf("unexpected status %d from Brave: %s", resp.StatusCode, summary)
	}
	return e
}

func addReset(details map[string]any, rl *RateLimit) {
	if rl == nil {
		return
	}
	if s, ok := rl.ResetSeconds(); ok {
		details["reset_seconds"] = s
	}
	details["rate_limit"] = rl
}

// isMissingTokenHeader recognises the VALIDATION error Brave returns when the
// X-Subscription-Token header is absent: meta.errors[].loc is
// ["header", "x-subscription-token"].
func isMissingTokenHeader(meta json.RawMessage) bool {
	// loc elements are strings or integer indexes (pydantic style), so they
	// are decoded loosely — one integer must not hide the header error.
	var m struct {
		Errors []struct {
			Loc  []any  `json:"loc"`
			Type string `json:"type"`
		} `json:"errors"`
	}
	if json.Unmarshal(meta, &m) != nil {
		return false
	}
	for _, e := range m.Errors {
		for _, l := range e.Loc {
			if s, ok := l.(string); ok && strings.EqualFold(s, "x-subscription-token") && e.Type == "missing" {
				return true
			}
		}
	}
	return false
}

// transportError maps a failed exchange (no response) onto an *Error.
func transportError(endpoint string, err error) *Error {
	details := map[string]any{"endpoint": endpoint}
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return &Error{Code: CodeTimeout, Message: "the request to Brave exceeded its deadline: " + err.Error(), Details: details}
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Code: CodeNetwork, Message: "the request to Brave was cancelled", Details: details}
	}
	return &Error{Code: CodeNetwork, Message: "could not reach Brave: " + err.Error(), Details: details}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func decodeError(endpoint string, err error) *Error {
	return &Error{Code: CodeDecode, Message: fmt.Sprintf("Brave's %s response was not the JSON expected: %v", endpoint, err),
		Details: map[string]any{"endpoint": endpoint}}
}
