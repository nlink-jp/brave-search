package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type codedErr struct {
	code    string
	msg     string
	details map[string]any
}

func (e *codedErr) Error() string                { return e.msg }
func (e *codedErr) ErrorCode() string            { return e.code }
func (e *codedErr) ErrorDetails() map[string]any { return e.details }

type echoArgs struct {
	Query string `json:"query"`
	Count int    `json:"count"`
}

func echoTool(fail error) Tool {
	return Tool{
		Name:        "echo",
		Description: "Echoes its arguments back, for exercising the protocol layer in tests.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"query": map[string]any{"type": "string"}, "count": map[string]any{"type": "integer"}},
			"required":   []string{"query"},
		},
		Handle: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a echoArgs
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if a.Query == "" {
				return nil, ArgErrorf("query is required")
			}
			if fail != nil {
				return nil, fail
			}
			return a, nil
		},
	}
}

// converse runs a whole stdio session and returns every response.
func converse(t *testing.T, s *Server, lines ...string) []map[string]any {
	t.Helper()
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out bytes.Buffer
	if err := s.Serve(context.Background(), in, &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("non-JSON line on stdout: %q", line)
		}
		responses = append(responses, m)
	}
	return responses
}

func toolPayload(t *testing.T, resp map[string]any) (map[string]any, bool) {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", resp)
	}
	content := result["content"].([]any)[0].(map[string]any)
	isErr, _ := result["isError"].(bool)
	var payload map[string]any
	if err := json.Unmarshal([]byte(content["text"].(string)), &payload); err != nil {
		t.Fatalf("tool text is not JSON: %v", err)
	}
	return payload, isErr
}

func TestLifecycle(t *testing.T) {
	s := New("1.2.3", echoTool(nil))
	responses := converse(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo","arguments":{"query":"x","count":2}}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":4}}`,
	)
	if len(responses) != 4 {
		t.Fatalf("got %d responses, want 4 (notifications are silent)", len(responses))
	}
	init := responses[0]["result"].(map[string]any)
	if init["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v", init["protocolVersion"])
	}
	if info := init["serverInfo"].(map[string]any); info["name"] != ServerName || info["version"] != "1.2.3" {
		t.Errorf("serverInfo = %v", info)
	}
	if init["instructions"] != Instructions {
		t.Error("initialize did not carry the instructions")
	}
	tools := responses[2]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 || tools[1].(map[string]any)["name"] != ToolGetUsage {
		t.Errorf("tools/list = %v", tools)
	}
	payload, isErr := toolPayload(t, responses[3])
	if isErr || payload["query"] != "x" || payload["count"].(float64) != 2 {
		t.Errorf("echo result = %v (isError=%v)", payload, isErr)
	}
}

func TestGetUsageReturnsTheManualVerbatim(t *testing.T) {
	s := New("dev")
	responses := converse(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_usage"}}`)
	result := responses[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if text != UsageDoc() {
		t.Error("get_usage did not return usage.md verbatim")
	}
}

func TestMissingAndUnknownArgumentsAreInvalidArguments(t *testing.T) {
	s := New("dev", echoTool(nil))
	for _, call := range []string{
		`{"name":"echo","arguments":{}}`,
		`{"name":"echo","arguments":{"query":"x","qeury":"y"}}`,
	} {
		responses := converse(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+call+`}`)
		payload, isErr := toolPayload(t, responses[0])
		if !isErr || payload["code"] != CodeInvalidArguments {
			t.Errorf("%s: %v (isError=%v)", call, payload, isErr)
		}
	}
}

func TestUpstreamCodesAndDetailsSurvive(t *testing.T) {
	fail := &codedErr{code: "rate_limited", msg: "slow down", details: map[string]any{"reset_seconds": 3}}
	s := New("dev", echoTool(fail))
	responses := converse(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"query":"x"}}}`)
	payload, isErr := toolPayload(t, responses[0])
	if !isErr || payload["code"] != "rate_limited" {
		t.Errorf("payload = %v", payload)
	}
	if d, _ := payload["details"].(map[string]any); d["reset_seconds"] != float64(3) {
		t.Errorf("details = %v", payload["details"])
	}
	if _, ok := responses[0]["error"]; ok {
		t.Error("a tool failure was returned as a JSON-RPC error")
	}
}

func TestPlainErrorsFallBackToInvalidArguments(t *testing.T) {
	s := New("dev", echoTool(errors.New("something odd")))
	responses := converse(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"query":"x"}}}`)
	payload, _ := toolPayload(t, responses[0])
	if payload["code"] != CodeInvalidArguments || payload["message"] != "something odd" {
		t.Errorf("payload = %v", payload)
	}
}

func TestUnknownToolAndMethod(t *testing.T) {
	s := New("dev")
	responses := converse(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"nosuch/method"}`,
		`{"jsonrpc":"2.0","method":"nosuch/notification"}`,
	)
	if len(responses) != 2 {
		t.Fatalf("got %d responses, want 2", len(responses))
	}
	payload, isErr := toolPayload(t, responses[0])
	if !isErr || !strings.Contains(payload["message"].(string), "nope") {
		t.Errorf("unknown tool: %v", payload)
	}
	if _, ok := responses[1]["error"]; !ok {
		t.Error("an unknown method should be a JSON-RPC error")
	}
}

// A malformed frame must be answered, or the client waits forever.
func TestMalformedFrameGetsAParseErrorAndTheSessionContinues(t *testing.T) {
	s := New("dev")
	responses := converse(t, s, `{not json`, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if len(responses) != 2 {
		t.Fatalf("got %d responses, want 2", len(responses))
	}
	errObj, ok := responses[0]["error"].(map[string]any)
	if !ok || errObj["code"].(float64) != -32700 {
		t.Errorf("malformed frame response = %v", responses[0])
	}
	if _, ok := responses[1]["result"]; !ok {
		t.Error("the session did not continue after a malformed frame")
	}
}

func TestNilStdinIsAnError(t *testing.T) {
	if err := New("dev").Serve(context.Background(), nil, &bytes.Buffer{}); err == nil {
		t.Error("nil stdin was accepted")
	}
}
