package brave

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Every body below is synthetic: the shapes follow the Brave documentation,
// the content is invented. No live response is stored here (Brave ToS).

const webBody = `{
  "type": "search",
  "query": {"original": "go generics", "altered": "go generics", "more_results_available": true},
  "web": {"type": "search", "results": [
    {"title": "Example One", "url": "https://example.com/one", "description": "First synthetic result.",
     "age": "2 days ago", "page_age": "2026-09-10T00:00:00", "language": "en",
     "extra_snippets": ["extra a", "extra b"], "thumbnail": {"src": "https://example.com/t.png"}},
    {"title": "Example Two", "url": "https://example.com/two", "description": "Second synthetic result."}
  ]}
}`

const contextBody = `{
  "grounding": {"generic": [
    {"url": "https://example.com/a", "title": "A", "snippets": ["chunk one", "chunk two"]},
    {"url": "https://example.com/b", "title": "B", "snippets": ["chunk three"]}
  ], "poi": null, "map": []},
  "sources": {
    "https://example.com/a": {"title": "A", "hostname": "example.com", "age": ["Monday", "2026-09-08"]},
    "https://example.com/b": {"title": "B", "hostname": "example.com", "age": "2026-09-09"}
  }
}`

type capture struct {
	method string
	path   string
	query  map[string]string
	header http.Header
}

func serve(t *testing.T, status int, body string, headers map[string]string) (*Client, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.method, cap.path, cap.header = r.Method, r.URL.Path, r.Header.Clone()
		cap.query = map[string]string{}
		for k := range r.URL.Query() {
			cap.query[k] = r.URL.Query().Get(k)
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "test-key", "", 5*time.Second, "brave-search/test"), cap
}

func TestWebSearchSendsTheDocumentedRequest(t *testing.T) {
	c, cap := serve(t, 200, webBody, map[string]string{
		"X-RateLimit-Limit": "1, 2000", "X-RateLimit-Remaining": "0, 1999",
		"X-RateLimit-Reset": "1, 86400", "X-RateLimit-Policy": "1;w=1, 2000;w=2592000",
	})
	c.APIVersion = "2026-09-12"
	res, meta, err := c.WebSearch(context.Background(), WebParams{
		Query: "go generics", Count: 5, Offset: 2, Country: "JP", SearchLang: "ja",
		Freshness: "pw", Safesearch: "strict", ExtraSnippets: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.method != "GET" || cap.path != "/web/search" {
		t.Errorf("%s %s", cap.method, cap.path)
	}
	for k, want := range map[string]string{
		"q": "go generics", "count": "5", "offset": "2", "country": "JP", "search_lang": "ja",
		"freshness": "pw", "safesearch": "strict", "extra_snippets": "true",
		"result_filter": "web", "text_decorations": "false",
	} {
		if cap.query[k] != want {
			t.Errorf("query %s = %q, want %q", k, cap.query[k], want)
		}
	}
	for k, want := range map[string]string{
		"X-Subscription-Token": "test-key", "Api-Version": "2026-09-12",
		"Accept": "application/json", "User-Agent": "brave-search/test",
	} {
		if got := cap.header.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	if len(res.Web.Results) != 2 || res.Web.Results[0].Title != "Example One" || res.Web.Results[0].ExtraSnippets[1] != "extra b" {
		t.Errorf("results = %+v", res.Web.Results)
	}
	if !strings.Contains(string(res.Web.Results[0].Raw), `"thumbnail"`) {
		t.Error("the raw result did not keep the undocumented field")
	}
	if !res.Query.MoreResultsAvailable || res.Query.Altered != "go generics" {
		t.Errorf("query = %+v", res.Query)
	}
	if meta.Status != 200 || meta.RateLimit == nil {
		t.Fatalf("meta = %+v", meta)
	}
	if got := meta.RateLimit.Remaining; len(got) != 2 || got[1] != 1999 {
		t.Errorf("remaining = %v", got)
	}
	if s, ok := meta.RateLimit.ResetSeconds(); !ok || s != 1 {
		t.Errorf("ResetSeconds = %d,%v (the exhausted window is the per-second one)", s, ok)
	}
}

func TestOptionalParametersAreOmittedWhenUnset(t *testing.T) {
	c, cap := serve(t, 200, webBody, nil)
	if _, _, err := c.WebSearch(context.Background(), WebParams{Query: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"count", "offset", "country", "search_lang", "freshness", "safesearch", "extra_snippets"} {
		if _, present := cap.query[k]; present {
			t.Errorf("%s was sent although unset", k)
		}
	}
	if cap.header.Get("Api-Version") != "" {
		t.Error("Api-Version was sent although unset")
	}
}

func TestLLMContextSendsTheDocumentedRequest(t *testing.T) {
	c, cap := serve(t, 200, contextBody, nil)
	res, _, err := c.LLMContext(context.Background(), ContextParams{
		Query: "q", Count: 7, Country: "US", SearchLang: "en", Freshness: "pd", Safesearch: "off", MaxTokens: 2048, MaxURLs: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.path != "/llm/context" {
		t.Errorf("path = %s", cap.path)
	}
	for k, want := range map[string]string{
		"q": "q", "count": "7", "country": "US", "search_lang": "en", "freshness": "pd", "safesearch": "off",
		"maximum_number_of_tokens": "2048", "maximum_number_of_urls": "3",
	} {
		if cap.query[k] != want {
			t.Errorf("query %s = %q, want %q", k, cap.query[k], want)
		}
	}
	if len(res.Grounding.Generic) != 2 || res.Grounding.Generic[0].Snippets[1] != "chunk two" {
		t.Errorf("grounding = %+v", res.Grounding)
	}
	// age arrives as a list for one source and a string for the other.
	if got := res.Sources["https://example.com/a"].Age; len(got) != 2 {
		t.Errorf("list age = %v", got)
	}
	if got := res.Sources["https://example.com/b"].Age; len(got) != 1 || got[0] != "2026-09-09" {
		t.Errorf("string age = %v", got)
	}
}

func TestMissingKeySendsNothing(t *testing.T) {
	c, cap := serve(t, 200, webBody, nil)
	c.APIKey = ""
	_, _, err := c.WebSearch(context.Background(), WebParams{Query: "x"})
	if Code(err) != CodeMissingAPIKey {
		t.Fatalf("err = %v", err)
	}
	if cap.method != "" {
		t.Error("a request was sent without a key")
	}
}

func TestStatusMapping(t *testing.T) {
	errBody := `{"type":"ErrorResponse","error":{"id":"x","status":0,"code":"UPSTREAM_CODE","detail":"the detail","meta":{"k":"v"}},"time":1}`
	for _, tc := range []struct {
		status int
		want   string
	}{
		{401, CodeUnauthorized},
		{403, CodePlanNotSubscribed},
		{400, CodeInvalidArguments},
		{422, CodeInvalidArguments},
		{429, CodeRateLimited},
		{500, CodeUpstream},
		{503, CodeUpstream},
		{302, CodeUpstream},
	} {
		c, _ := serve(t, tc.status, errBody, map[string]string{"X-RateLimit-Reset": "7", "X-RateLimit-Remaining": "0"})
		// httptest follows no redirects for 302 without Location, so the
		// client sees the status itself.
		_, meta, err := c.WebSearch(context.Background(), WebParams{Query: "x"})
		var e *Error
		if !errors.As(err, &e) || e.Code != tc.want {
			t.Errorf("%d: code = %v, want %s (err=%v)", tc.status, Code(err), tc.want, err)
			continue
		}
		if e.Status != tc.status || e.Details["upstream_code"] != "UPSTREAM_CODE" || e.Details["upstream_detail"] != "the detail" {
			t.Errorf("%d: details = %v", tc.status, e.Details)
		}
		if !strings.Contains(e.Message, "the detail") {
			t.Errorf("%d: message lacks the upstream detail: %q", tc.status, e.Message)
		}
		if tc.status == 429 {
			if e.Details["reset_seconds"] != 7 {
				t.Errorf("429: reset_seconds = %v", e.Details["reset_seconds"])
			}
		}
		if meta == nil || meta.Status != tc.status {
			t.Errorf("%d: meta = %+v", tc.status, meta)
		}
	}
}

func TestNonJSONErrorBodyStillMaps(t *testing.T) {
	c, _ := serve(t, 502, "<html>bad gateway</html>", nil)
	_, _, err := c.WebSearch(context.Background(), WebParams{Query: "x"})
	if Code(err) != CodeUpstream || !strings.Contains(err.Error(), "bad gateway") {
		t.Errorf("err = %v", err)
	}
}

func TestDecodeError(t *testing.T) {
	c, _ := serve(t, 200, "not json", nil)
	_, _, err := c.WebSearch(context.Background(), WebParams{Query: "x"})
	if Code(err) != CodeDecode {
		t.Errorf("err = %v", err)
	}
}

func TestTimeoutIsItsOwnCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "k", "", 50*time.Millisecond, "")
	_, _, err := c.WebSearch(context.Background(), WebParams{Query: "x"})
	if Code(err) != CodeTimeout {
		t.Errorf("err = %v", err)
	}
	// A caller's own deadline maps the same way.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, _, err = New(srv.URL, "k", "", time.Minute, "").WebSearch(ctx, WebParams{Query: "x"})
	if Code(err) != CodeTimeout {
		t.Errorf("ctx deadline: err = %v", err)
	}
}

func TestUnreachableIsNetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1", "k", "", time.Second, "")
	_, _, err := c.WebSearch(context.Background(), WebParams{Query: "x"})
	if Code(err) != CodeNetwork {
		t.Errorf("err = %v", err)
	}
}

func TestWithTimeoutDoesNotAliasTheOriginal(t *testing.T) {
	c := New("http://x", "k", "", time.Second, "")
	d := c.WithTimeout(time.Minute)
	if c.HTTP.(*http.Client).Timeout != time.Second || d.HTTP.(*http.Client).Timeout != time.Minute {
		t.Error("WithTimeout changed the original or not the copy")
	}
}

func TestRateLimitParsing(t *testing.T) {
	h := http.Header{}
	if parseRateLimit(h) != nil {
		t.Error("no headers should give nil")
	}
	h.Set("X-RateLimit-Limit", "1, junk, 15000")
	h.Set("X-RateLimit-Reset", "1, 42")
	rl := parseRateLimit(h)
	if len(rl.Limit) != 2 || rl.Limit[1] != 15000 {
		t.Errorf("limit = %v", rl.Limit)
	}
	if s, ok := rl.ResetSeconds(); !ok || s != 1 {
		t.Errorf("with no remaining counts the first reset wins: %d,%v", s, ok)
	}
}
