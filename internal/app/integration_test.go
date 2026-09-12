package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nlink-jp/brave-search/internal/config"
)

// These tests exercise the whole path — flags → config → engine → client →
// output — against an httptest upstream reached through BRAVE_SEARCH_BASE_URL.
// Bodies are synthetic (Brave ToS: no live response is stored).

const webBody = `{"type":"search","query":{"original":"go generic","altered":"go generics","more_results_available":true},
 "web":{"results":[{"title":"Title One","url":"https://example.com/1","description":"Desc one.","age":"3 days ago","language":"en","extra_snippets":["extra one"]},
                   {"title":"Title Two","url":"https://example.com/2","description":"Desc two."}]}}`

const contextBody = `{"grounding":{"generic":[{"url":"https://example.com/a","title":"Alpha","snippets":["first chunk","second chunk"]}]},
 "sources":{"https://example.com/a":{"title":"Alpha","hostname":"example.com","age":["2026-09-08"]}}}`

type upstream struct {
	status int
	last   *http.Request
}

func startUpstream(t *testing.T, u *upstream) {
	t.Helper()
	isolate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.last = r
		w.Header().Set("X-RateLimit-Limit", "1, 2000")
		w.Header().Set("X-RateLimit-Remaining", "0, 1234")
		w.Header().Set("X-RateLimit-Reset", "1, 999")
		if u.status != 0 && u.status != 200 {
			w.WriteHeader(u.status)
			_, _ = w.Write([]byte(`{"type":"ErrorResponse","error":{"code":"SOME_CODE","detail":"synthetic failure"}}`))
			return
		}
		switch r.URL.Path {
		case "/web/search":
			_, _ = w.Write([]byte(webBody))
		case "/llm/context":
			_, _ = w.Write([]byte(contextBody))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(config.EnvBaseURL, srv.URL)
	t.Setenv(config.EnvAPIKey, "test-key")
}

func TestWebTextOutput(t *testing.T) {
	u := &upstream{}
	startUpstream(t, u)
	var stdout, stderr bytes.Buffer
	code := run([]string{"web", "go", "generic", "--count", "2", "--extra-snippets"}, "t", nil, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if got := u.last.Header.Get("X-Subscription-Token"); got != "test-key" {
		t.Errorf("key header = %q", got)
	}
	if q := u.last.URL.Query(); q.Get("q") != "go generic" || q.Get("count") != "2" || q.Get("extra_snippets") != "true" {
		t.Errorf("query = %v", q)
	}
	out := stdout.String()
	for _, want := range []string{"# go generic", "(searched as: go generics)", "1. Title One", "https://example.com/1", "Desc one.",
		"3 days ago · lang: en", "+ extra one", "2. Title Two", "more results available: --offset 1", "cost: $0.0050", "0/1, 1234/2000"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr: %s", stderr.String())
	}
}

func TestWebJSONOutput(t *testing.T) {
	u := &upstream{}
	startUpstream(t, u)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"web", "--json", "go generic", "--full"}, "t", nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"altered": "go generics"`, `"rank": 1`, `"full": [`, `"cost_basis": "list_price_estimate"`, `"reset_seconds": [`} {
		if !strings.Contains(out, want) {
			t.Errorf("json lacks %q:\n%s", want, out)
		}
	}
}

func TestContextOutputs(t *testing.T) {
	u := &upstream{}
	startUpstream(t, u)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"context", "what is alpha", "--max-tokens", "2048", "--max-urls", "3"}, "t", nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if q := u.last.URL.Query(); q.Get("maximum_number_of_tokens") != "2048" || q.Get("maximum_number_of_urls") != "3" {
		t.Errorf("query = %v", q)
	}
	out := stdout.String()
	for _, want := range []string{"(1 URLs, 2 snippets)", "## [1] Alpha", "example.com · 2026-09-08", "first chunk", "second chunk", "cost: $0.0050"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	stdout.Reset()
	if code := run([]string{"context", "--json", "what is alpha"}, "t", nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"snippets": 2`) {
		t.Errorf("json: %s", stdout.String())
	}
}

func TestExitContract(t *testing.T) {
	// Upstream failure → 1, with the reset hint for a 429.
	u := &upstream{status: 429}
	startUpstream(t, u)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"web", "q"}, "t", nil, &stdout, &stderr); code != exitUpstream {
		t.Errorf("429: exit %d, want %d", code, exitUpstream)
	}
	if !strings.Contains(stderr.String(), "synthetic failure") || !strings.Contains(stderr.String(), "resets in 1s") {
		t.Errorf("429 stderr: %s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Error("a failure wrote to stdout")
	}

	// Local validation → 2, and nothing was sent.
	u.last, u.status = nil, 200
	stderr.Reset()
	if code := run([]string{"web", "q", "--count", "99"}, "t", nil, &stdout, &stderr); code != exitError {
		t.Errorf("bad count: exit %d, want %d", code, exitError)
	}
	if u.last != nil {
		t.Error("a request was sent for an invalid count")
	}

	// Missing query → 2.
	if code := run([]string{"web"}, "t", nil, &stdout, &stderr); code != exitError {
		t.Errorf("no query: exit %d, want %d", code, exitError)
	}

	// Missing key → 2, nothing sent.
	t.Setenv(config.EnvAPIKey, "")
	u.last = nil
	stderr.Reset()
	if code := run([]string{"context", "q"}, "t", nil, &stdout, &stderr); code != exitError {
		t.Errorf("no key: exit %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "BRAVE_SEARCH_API_KEY") || u.last != nil {
		t.Errorf("no key: stderr=%q sent=%v", stderr.String(), u.last != nil)
	}
}

func TestFlagsAfterTheQueryAreNotDropped(t *testing.T) {
	u := &upstream{}
	startUpstream(t, u)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"web", "q", "--offset", "2", "--country", "jp", "--lang", "ja", "--safesearch", "off", "--freshness", "pw"}, "t", nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	q := u.last.URL.Query()
	for k, want := range map[string]string{"offset": "2", "country": "JP", "search_lang": "ja", "safesearch": "off", "freshness": "pw"} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
}

func TestConfigFileDefaultsReachTheRequest(t *testing.T) {
	u := &upstream{}
	startUpstream(t, u)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := writeFile(path, "[search]\ncountry = \"JP\"\nsearch_lang = \"ja\"\ncount = 4\n\n[context]\nmax_tokens = 4096\n"); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"context", "q", "--config", path}, "t", nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	q := u.last.URL.Query()
	if q.Get("country") != "JP" || q.Get("search_lang") != "ja" || q.Get("count") != "4" || q.Get("maximum_number_of_tokens") != "4096" {
		t.Errorf("query = %v", q)
	}
}

// The MCP face goes through the same engine: the same synthetic upstream
// answers a tools/call the same way the CLI was answered.
func TestMCPToolsCallTheSameEngine(t *testing.T) {
	u := &upstream{}
	startUpstream(t, u)
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"web_search","arguments":{"query":"go generic","count":2}}}` + "\n" +
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"llm_context","arguments":{"query":"alpha","max_tokens":2048}}}` + "\n" +
			`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"web_search","arguments":{"query":"x","count":50}}}` + "\n")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"mcp"}, "t", in, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines: %s", len(lines), stdout.String())
	}
	for _, want := range []string{`"web_search"`, `"llm_context"`, `"get_usage"`} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("tools/list lacks %s", want)
		}
	}
	if !strings.Contains(lines[1], `\"altered\": \"go generics\"`) || !strings.Contains(lines[1], `\"cost_usd\": 0.005`) {
		t.Errorf("web_search result: %s", lines[1])
	}
	if !strings.Contains(lines[2], `\"snippets\": 2`) {
		t.Errorf("llm_context result: %s", lines[2])
	}
	if !strings.Contains(lines[3], `"isError":true`) || !strings.Contains(lines[3], `invalid_arguments`) {
		t.Errorf("bad count: %s", lines[3])
	}
}
