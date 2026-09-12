package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nlink-jp/brave-search/internal/config"
)

// Synthetic SSE streams; see answers_test.go in internal/brave for the shape.
const answerStream = "data: {\"choices\":[{\"delta\":{\"content\":\"Generics add type parameters.<citation>{\\\"number\\\":1,\\\"url\\\":\\\"https://example.com/g\\\",\\\"snippet\\\":\\\"type parameters\\\"}</citation>\"},\"index\":0}]}\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\"<usage>{\\\"X-Request-Requests\\\":1,\\\"X-Request-Queries\\\":1,\\\"X-Request-Tokens-In\\\":80,\\\"X-Request-Tokens-Out\\\":12,\\\"X-Request-Total-Cost\\\":0.00446}</usage>\"},\"index\":0,\"finish_reason\":\"stop\"}]}\n" +
	"data: [DONE]\n"

const researchStream = "data: {\"choices\":[{\"delta\":{\"content\":\"<progress>{\\\"iteration\\\":1,\\\"queries\\\":3}</progress>\"},\"index\":0}]}\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\"<progress>{\\\"iteration\\\":2,\\\"queries\\\":6}</progress><blindspots>Pricing after 2026 was not covered.</blindspots>\"},\"index\":0}]}\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\"<answer>Alpha beats Beta on X.<citation>{\\\"number\\\":1,\\\"url\\\":\\\"https://example.com/a\\\"}</citation></answer>\"},\"index\":0}]}\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\"<usage>{\\\"X-Request-Requests\\\":1,\\\"X-Request-Queries\\\":6,\\\"X-Request-Tokens-In\\\":5000,\\\"X-Request-Tokens-Out\\\":400,\\\"X-Request-Total-Cost\\\":0.051}</usage>\"},\"index\":0}]}\n" +
	"data: [DONE]\n"

type answersUpstream struct {
	sent  map[string]any
	token string
}

func startAnswersUpstream(t *testing.T, u *answersUpstream) {
	t.Helper()
	isolate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			w.WriteHeader(404)
			return
		}
		u.token = r.Header.Get("X-Subscription-Token")
		b, _ := io.ReadAll(r.Body)
		u.sent = map[string]any{}
		_ = json.Unmarshal(b, &u.sent)
		if u.sent["enable_research"] == true {
			_, _ = w.Write([]byte(researchStream))
		} else {
			_, _ = w.Write([]byte(answerStream))
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(config.EnvBaseURL, srv.URL)
	t.Setenv(config.EnvAPIKey, "search-key")
}

func TestAnswerTextAndJSON(t *testing.T) {
	u := &answersUpstream{}
	startAnswersUpstream(t, u)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"answer", "what", "are", "generics", "--max-tokens", "200", "--lang", "ja"}, "t", nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if u.sent["max_completion_tokens"] != float64(200) || u.sent["language"] != "ja" || u.sent["enable_citations"] != true {
		t.Errorf("sent %v", u.sent)
	}
	out := stdout.String()
	for _, want := range []string{"# what are generics", "Generics add type parameters.", "Sources:", "[1] https://example.com/g — type parameters",
		"cost: $0.0045 (reported by Brave) · requests: 1 · searches: 1 · tokens: 80 in / 12 out"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr: %s", stderr.String())
	}
	stdout.Reset()
	if code := run([]string{"answer", "--json", "what are generics"}, "t", nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for _, want := range []string{`"mode": "answer"`, `"cost_basis": "reported_by_brave"`, `"start_index"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("json lacks %q", want)
		}
	}
}

func TestResearchStreamsProgressToStderr(t *testing.T) {
	u := &answersUpstream{}
	startAnswersUpstream(t, u)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"research", "alpha vs beta", "--max-queries", "3", "--max-iterations", "1", "--max-seconds", "30"}, "t", nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if u.sent["research_maximum_number_of_queries"] != float64(3) || u.sent["research_maximum_number_of_seconds"] != float64(30) {
		t.Errorf("sent %v", u.sent)
	}
	if _, ok := u.sent["enable_citations"]; ok {
		t.Error("enable_citations sent with research")
	}
	progress := stderr.String()
	if strings.Count(progress, "research: ") != 2 || !strings.Contains(progress, `"iteration":2`) {
		t.Errorf("progress on stderr = %q", progress)
	}
	out := stdout.String()
	for _, want := range []string{"Alpha beats Beta on X.", "[1] https://example.com/a", "Blind spots (declared by Brave):", "Pricing after 2026 was not covered.", "searches: 6"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "research: ") {
		t.Error("progress leaked into stdout")
	}
}

func TestAnswersKeyFromConfigReachesTheRequest(t *testing.T) {
	u := &answersUpstream{}
	startAnswersUpstream(t, u)
	t.Setenv(config.EnvAnswersAPIKey, "answers-key")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"answer", "q"}, "t", nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if u.token != "answers-key" {
		t.Errorf("token = %q", u.token)
	}
}

func TestAnswersMCPTools(t *testing.T) {
	u := &answersUpstream{}
	startAnswersUpstream(t, u)
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"answer","arguments":{"question":"q"}}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"research","arguments":{"question":"q","max_iterations":1}}}` + "\n" +
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"research","arguments":{"question":"q","max_seconds":999}}}` + "\n")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"mcp"}, "t", in, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines", len(lines))
	}
	if !strings.Contains(lines[0], `\"mode\": \"answer\"`) || !strings.Contains(lines[0], `\"cost_usd\": 0.00446`) {
		t.Errorf("answer: %s", lines[0])
	}
	if !strings.Contains(lines[1], `\"blindspots\"`) || !strings.Contains(lines[1], `\"searches\": 6`) {
		t.Errorf("research: %s", lines[1])
	}
	if !strings.Contains(lines[2], `"isError":true`) || !strings.Contains(lines[2], "max_seconds") {
		t.Errorf("bad max_seconds: %s", lines[2])
	}
	if stderr.Len() != 0 {
		t.Errorf("the MCP server wrote progress to stderr: %s", stderr.String())
	}
}
