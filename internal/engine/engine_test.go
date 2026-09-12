package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nlink-jp/brave-search/internal/brave"
	"github.com/nlink-jp/brave-search/internal/config"
)

// Synthetic bodies — shapes per the documentation, content invented.
const webBody = `{"type":"search","query":{"original":"go generic","altered":"go generics","more_results_available":true},
 "web":{"results":[{"title":"T1","url":"https://example.com/1","description":"D1","age":"1 day ago","language":"en","extra_snippets":["x"],"extra":1},
                   {"title":"T2","url":"https://example.com/2","description":"D2"}]}}`

const contextBody = `{"grounding":{"generic":[{"url":"https://example.com/a","title":"","snippets":["s1","s2"]},{"url":"https://example.com/b","title":"B","snippets":["s3"]}]},
 "sources":{"https://example.com/a":{"title":"A from source","hostname":"example.com","age":["Monday","2026-09-08","4 days ago"]}}}`

func newEngine(t *testing.T, body string, seen *map[string]string) *Engine {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			m := map[string]string{}
			for k := range r.URL.Query() {
				m[k] = r.URL.Query().Get(k)
			}
			*seen = m
		}
		w.Header().Set("X-RateLimit-Limit", "1, 2000")
		w.Header().Set("X-RateLimit-Remaining", "1, 1500")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(config.Defaults(), brave.New(srv.URL, "k", "", 5*time.Second, "t"))
}

func TestWebAppliesDefaultsAndShapesTheResult(t *testing.T) {
	var seen map[string]string
	e := newEngine(t, webBody, &seen)
	res, err := e.Web(context.Background(), WebRequest{Query: "go generic", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"count": "10", "country": "US", "search_lang": "en", "safesearch": "moderate"} {
		if seen[k] != want {
			t.Errorf("sent %s=%q, want %q", k, seen[k], want)
		}
	}
	if res.Query != "go generic" || res.Altered != "go generics" || !res.MoreResultsAvailable || res.Count != 2 {
		t.Errorf("result header: %+v", res)
	}
	if res.Results[0].Rank != 1 || res.Results[1].Rank != 2 || res.Results[0].ExtraSnippets[0] != "x" {
		t.Errorf("hits: %+v", res.Results)
	}
	if len(res.Full) != 2 || !strings.Contains(string(res.Full[0]), `"extra":1`) {
		t.Errorf("full: %s", res.Full)
	}
	if res.Meta.Requests != 1 || res.Meta.CostUSD != SearchRequestUSD || res.Meta.CostBasis != CostEstimate || res.Meta.RateLimit == nil {
		t.Errorf("meta: %+v", res.Meta)
	}
	if !strings.Contains(res.Meta.String(), "1/1, 1500/2000") {
		t.Errorf("meta line: %s", res.Meta.String())
	}
}

func TestWebRankFollowsThePage(t *testing.T) {
	e := newEngine(t, webBody, nil)
	res, err := e.Web(context.Background(), WebRequest{Query: "q", Count: 2, Offset: 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.Results[0].Rank != 7 {
		t.Errorf("rank on page 3 of 2 = %d, want 7", res.Results[0].Rank)
	}
	if len(res.Full) != 0 {
		t.Error("full was populated without --full")
	}
}

func TestAlteredIsOmittedWhenIdentical(t *testing.T) {
	e := newEngine(t, `{"query":{"original":"x","altered":"x"},"web":{"results":[]}}`, nil)
	res, err := e.Web(context.Background(), WebRequest{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Altered != "" || res.Count != 0 || res.Results == nil {
		t.Errorf("%+v", res)
	}
}

func TestContextFoldsSourcesIn(t *testing.T) {
	var seen map[string]string
	e := newEngine(t, contextBody, &seen)
	res, err := e.Context(context.Background(), ContextRequest{Query: "q", MaxTokens: 2048})
	if err != nil {
		t.Fatal(err)
	}
	if seen["maximum_number_of_tokens"] != "2048" || seen["maximum_number_of_urls"] != "20" || seen["count"] != "10" {
		t.Errorf("sent %v", seen)
	}
	if res.URLs != 2 || res.Snippets != 3 {
		t.Errorf("counts: %+v", res)
	}
	a := res.Chunks[0]
	if a.Title != "A from source" || a.Hostname != "example.com" || a.Age != "2026-09-08" {
		t.Errorf("chunk a: %+v", a)
	}
	if b := res.Chunks[1]; b.Title != "B" || b.Age != "" {
		t.Errorf("chunk b: %+v", b)
	}
}

func TestValidationRefusesBeforeSpendingARequest(t *testing.T) {
	var seen map[string]string
	e := newEngine(t, webBody, &seen)
	long := strings.Repeat("w ", 51)
	for name, r := range map[string]WebRequest{
		"empty":      {Query: "  "},
		"long":       {Query: strings.Repeat("x", 401)},
		"words":      {Query: long},
		"count":      {Query: "q", Count: 21},
		"offset":     {Query: "q", Offset: 10},
		"country":    {Query: "q", Country: "USA"},
		"lang":       {Query: "q", SearchLang: "e"},
		"freshness":  {Query: "q", Freshness: "yesterday"},
		"safesearch": {Query: "q", Safesearch: "maybe"},
	} {
		seen = nil
		_, err := e.Web(context.Background(), r)
		if brave.Code(err) != brave.CodeInvalidArguments {
			t.Errorf("%s: err = %v", name, err)
		}
		if seen != nil {
			t.Errorf("%s: a request was sent", name)
		}
	}
	for name, r := range map[string]ContextRequest{
		"count":  {Query: "q", Count: 51},
		"tokens": {Query: "q", MaxTokens: 100},
		"urls":   {Query: "q", MaxURLs: 99},
	} {
		seen = nil
		_, err := e.Context(context.Background(), r)
		if brave.Code(err) != brave.CodeInvalidArguments {
			t.Errorf("%s: err = %v", name, err)
		}
		if seen != nil {
			t.Errorf("%s: a request was sent", name)
		}
	}
}

func TestDateRangeFreshnessAndLowercaseCountryAreAccepted(t *testing.T) {
	var seen map[string]string
	e := newEngine(t, webBody, &seen)
	if _, err := e.Web(context.Background(), WebRequest{Query: "q", Freshness: "2026-01-01to2026-02-01", Country: "jp"}); err != nil {
		t.Fatal(err)
	}
	if seen["freshness"] != "2026-01-01to2026-02-01" || seen["country"] != "JP" {
		t.Errorf("sent %v", seen)
	}
}

func TestUpstreamErrorsPassThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	t.Cleanup(srv.Close)
	e := New(config.Defaults(), brave.New(srv.URL, "k", "", time.Second, "t"))
	if _, err := e.Web(context.Background(), WebRequest{Query: "q"}); brave.Code(err) != brave.CodeRateLimited {
		t.Errorf("err = %v", err)
	}
	if _, err := e.Context(context.Background(), ContextRequest{Query: "q"}); brave.Code(err) != brave.CodeRateLimited {
		t.Errorf("err = %v", err)
	}
}
