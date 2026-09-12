//go:build e2e

package e2e

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nlink-jp/brave-search/internal/brave"
	"github.com/nlink-jp/brave-search/internal/config"
	"github.com/nlink-jp/brave-search/internal/engine"
)

// Live tests against the real Brave Search API. They assert on responses and
// log what they measured; they never save a body (Brave ToS). Budget for a
// full run: 7 requests, or 8 with BRAVE_SEARCH_E2E_RESEARCH=1.
//
// Everything a test logs with "MEASURED:" answers an open question in
// AGENTS.md — copy the answer there with the date.

func live(t *testing.T) (*config.Config, *engine.Engine) {
	t.Helper()
	cfg, err := config.Load("", 0)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !cfg.HasKey() {
		t.Skip("no Brave API key configured; set [api] api_key or BRAVE_SEARCH_API_KEY")
	}
	c := brave.New(cfg.BaseURL, cfg.APIKey, cfg.APIVersion, cfg.Timeout, "brave-search/e2e")
	c.AnswersAPIKey = cfg.AnswersAPIKey
	return cfg, engine.New(cfg, c)
}

func needAnswers(t *testing.T, cfg *config.Config) {
	t.Helper()
	if cfg.AnswersAPIKey == "" {
		t.Skip("no Answers key configured; set [api] answers_api_key or BRAVE_SEARCH_ANSWERS_API_KEY")
	}
}

func TestLiveWebSearch(t *testing.T) {
	_, e := live(t)
	res, err := e.Web(context.Background(), engine.WebRequest{Query: "Brave Search API documentation", Count: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) == 0 {
		t.Fatal("no results for a query that certainly has some")
	}
	for _, h := range res.Results {
		if h.URL == "" || h.Title == "" {
			t.Errorf("result without url/title: %+v", h)
		}
	}
	if res.Meta.RateLimit == nil {
		t.Error("no X-RateLimit-* headers on /web/search")
	}
	t.Logf("MEASURED: web_search rate_limit=%+v more=%v", res.Meta.RateLimit, res.MoreResultsAvailable)
}

func TestLiveLLMContext(t *testing.T) {
	_, e := live(t)
	res, err := e.Context(context.Background(), engine.ContextRequest{Query: "what is the Brave Search API", Count: 3, MaxTokens: 1024, MaxURLs: 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.URLs == 0 || res.Snippets == 0 {
		t.Fatalf("empty context: %+v", res)
	}
	t.Logf("MEASURED: llm_context urls=%d snippets=%d rate_limit=%+v", res.URLs, res.Snippets, res.Meta.RateLimit)
}

// The auth probe rests on Brave refusing an invalid request with 4xx after
// accepting the key, and on that refusal being unbilled. This pins the first
// half; the second is a documentation claim to watch on the dashboard.
func TestLiveProbeDistinguishesKeyFromRequest(t *testing.T) {
	cfg, e := live(t)
	_, err := e.Client.Probe(context.Background(), brave.EndpointWebSearch)
	if got := brave.Code(err); got != brave.CodeInvalidArguments {
		t.Errorf("MEASURED: /web/search probe with a valid key answered %s (%v), want invalid_arguments — auth check's 'valid' reading is wrong if so", got, err)
	}
	bogus := *e.Client
	bogus.APIKey = "definitely-not-a-key"
	_, err = bogus.Probe(context.Background(), brave.EndpointWebSearch)
	if got := brave.Code(err); got != brave.CodeUnauthorized {
		t.Errorf("MEASURED: /web/search probe with a bogus key answered %s (%v), want unauthorized", got, err)
	}
	if cfg.AnswersAPIKey != "" {
		_, err = e.Client.Probe(context.Background(), brave.EndpointAnswers)
		t.Logf("MEASURED: /chat/completions probe with the Answers key answered code=%q err=%v", brave.Code(err), err)
		if brave.Code(err) != brave.CodeInvalidArguments {
			t.Errorf("Answers probe: want invalid_arguments")
		}
	}
}

// Whether an arbitrary Api-Version date is accepted decides the default.
func TestLiveAPIVersionHeader(t *testing.T) {
	_, e := live(t)
	c := *e.Client
	c.APIVersion = "2026-09-12"
	_, meta, err := c.WebSearch(context.Background(), brave.WebParams{Query: "Brave Search", Count: 1})
	t.Logf("MEASURED: Api-Version: 2026-09-12 → status=%d code=%q err=%v", metaStatus(meta), brave.Code(err), err)
}

func metaStatus(m *brave.Meta) int {
	if m == nil {
		return 0
	}
	return m.Status
}

func TestLiveAnswer(t *testing.T) {
	cfg, e := live(t)
	needAnswers(t, cfg)
	res, err := e.Answer(context.Background(), engine.AnswerRequest{Question: "What is the Brave Search API?", MaxTokens: 200})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Answer) == "" {
		t.Fatal("empty answer text")
	}
	if len(res.Citations) == 0 {
		t.Error("no citations although enable_citations was set — the tag shape may differ from the documentation")
	}
	if res.Meta.CostBasis != engine.CostReported {
		t.Errorf("no usage tag in the stream (cost_basis=%s) — the tag shape may differ", res.Meta.CostBasis)
	}
	t.Logf("MEASURED: answer citations=%d meta=%+v rate_limit_on_answers=%v", len(res.Citations), res.Meta, res.Meta.RateLimit != nil)
}

func TestLiveResearch(t *testing.T) {
	cfg, e := live(t)
	needAnswers(t, cfg)
	if os.Getenv("BRAVE_SEARCH_E2E_RESEARCH") != "1" {
		t.Skip("research is billed per search; set BRAVE_SEARCH_E2E_RESEARCH=1 to run it")
	}
	start := time.Now()
	var reports int
	res, err := e.Research(context.Background(), engine.ResearchRequest{
		Question: "What does the Brave Search API's research mode do?", MaxQueries: 2, MaxIterations: 1, MaxSeconds: 60,
	}, func(p brave.Progress) { reports++; t.Logf("progress: %s", p) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Answer) == "" {
		t.Fatal("empty research answer")
	}
	t.Logf("MEASURED: research elapsed=%s progress_reports=%d citations=%d blindspots_len=%d meta=%+v",
		time.Since(start).Round(time.Second), reports, len(res.Citations), len(res.Blindspots), res.Meta)
}
