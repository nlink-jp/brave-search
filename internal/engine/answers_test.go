package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nlink-jp/brave-search/internal/brave"
	"github.com/nlink-jp/brave-search/internal/config"
)

const answerStream = "data: {\"choices\":[{\"delta\":{\"content\":\"An answer.<citation>{\\\"number\\\":1,\\\"url\\\":\\\"https://example.com/1\\\"}</citation>\"},\"index\":0}]}\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\"<usage>{\\\"X-Request-Requests\\\":1,\\\"X-Request-Queries\\\":1,\\\"X-Request-Tokens-In\\\":50,\\\"X-Request-Tokens-Out\\\":10,\\\"X-Request-Total-Cost\\\":0.0043}</usage>\"},\"index\":0,\"finish_reason\":\"stop\"}]}\n" +
	"data: [DONE]\n"

const researchStream = "data: {\"choices\":[{\"delta\":{\"content\":\"<progress>{\\\"iteration\\\":1}</progress><blindspots>none found</blindspots><answer>Deep answer.<citation>{\\\"number\\\":1,\\\"url\\\":\\\"https://example.com/r\\\"}</citation></answer>\"},\"index\":0}]}\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\"<usage>{\\\"X-Request-Requests\\\":1,\\\"X-Request-Queries\\\":6,\\\"X-Request-Tokens-In\\\":4000,\\\"X-Request-Tokens-Out\\\":300,\\\"X-Request-Total-Cost\\\":0.0455}</usage>\"},\"index\":0}]}\n" +
	"data: [DONE]\n"

func answersEngine(t *testing.T, stream string, sent *map[string]any, timeoutSeen *time.Duration) *Engine {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sent != nil {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, sent)
		}
		_, _ = w.Write([]byte(stream))
	}))
	t.Cleanup(srv.Close)
	e := New(config.Defaults(), brave.New(srv.URL, "k", "", 5*time.Second, "t"))
	return e
}

func TestAnswerDefaultsAndShape(t *testing.T) {
	var sent map[string]any
	e := answersEngine(t, answerStream, &sent, nil)
	res, err := e.Answer(context.Background(), AnswerRequest{Question: "why"})
	if err != nil {
		t.Fatal(err)
	}
	if sent["country"] != "US" || sent["language"] != "en" || sent["safesearch"] != "moderate" || sent["enable_citations"] != true {
		t.Errorf("sent %v", sent)
	}
	if _, ok := sent["max_completion_tokens"]; ok {
		t.Error("max_completion_tokens was sent although the default is 0 (API's choice)")
	}
	if res.Mode != "answer" || res.Answer != "An answer." || len(res.Citations) != 1 {
		t.Errorf("%+v", res)
	}
	m := res.Meta
	if m.CostBasis != CostReported || m.CostUSD != 0.0043 || m.Searches != 1 || m.TokensIn != 50 || m.TokensOut != 10 || m.Requests != 1 {
		t.Errorf("meta %+v", m)
	}
	if s := m.String(); s != "cost: $0.0043 (reported by Brave) · requests: 1 · searches: 1 · tokens: 50 in / 10 out" {
		t.Errorf("meta line = %q", s)
	}
}

func TestResearchDefaultsAreTighterThanTheAPIs(t *testing.T) {
	var sent map[string]any
	e := answersEngine(t, researchStream, &sent, nil)
	var reports int
	res, err := e.Research(context.Background(), ResearchRequest{Question: "deep"}, func(brave.Progress) { reports++ })
	if err != nil {
		t.Fatal(err)
	}
	if sent["enable_research"] != true || sent["research_maximum_number_of_queries"] != float64(10) ||
		sent["research_maximum_number_of_iterations"] != float64(2) || sent["research_maximum_number_of_seconds"] != float64(120) {
		t.Errorf("sent %v", sent)
	}
	if res.Mode != "research" || res.Answer != "Deep answer." || res.Blindspots != "none found" || reports != 1 || len(res.Progress) != 1 {
		t.Errorf("%+v (reports=%d)", res, reports)
	}
	if res.Meta.Searches != 6 || res.Meta.CostUSD != 0.0455 {
		t.Errorf("meta %+v", res.Meta)
	}
}

func TestResearchDeadlineFollowsTheBudget(t *testing.T) {
	// A server that answers only after 200 ms; a 1-second budget gives a
	// 31-second deadline, so it succeeds, while the shared 50 ms timeout
	// would have failed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(researchStream))
	}))
	t.Cleanup(srv.Close)
	e := New(config.Defaults(), brave.New(srv.URL, "k", "", 50*time.Millisecond, "t"))
	if _, err := e.Research(context.Background(), ResearchRequest{Question: "q", MaxSeconds: 1}, nil); err != nil {
		t.Errorf("research was cut off by the shared timeout: %v", err)
	}
	if _, err := e.Answer(context.Background(), AnswerRequest{Question: "q"}); brave.Code(err) != brave.CodeTimeout {
		t.Errorf("answer should have hit the shared timeout: %v", err)
	}
}

func TestAnswersValidation(t *testing.T) {
	var sent map[string]any
	e := answersEngine(t, answerStream, &sent, nil)
	for name, r := range map[string]AnswerRequest{
		"empty":   {Question: ""},
		"country": {Question: "q", Country: "Japan"},
		"lang":    {Question: "q", Language: "j"},
		"tokens":  {Question: "q", MaxTokens: -5},
	} {
		sent = nil
		if _, err := e.Answer(context.Background(), r); brave.Code(err) != brave.CodeInvalidArguments {
			t.Errorf("%s: %v", name, err)
		}
		if sent != nil {
			t.Errorf("%s: a request was sent", name)
		}
	}
	for name, r := range map[string]ResearchRequest{
		"queries": {Question: "q", MaxQueries: 51},
		"iters":   {Question: "q", MaxIterations: 6},
		"seconds": {Question: "q", MaxSeconds: 301},
	} {
		sent = nil
		if _, err := e.Research(context.Background(), r, nil); brave.Code(err) != brave.CodeInvalidArguments {
			t.Errorf("%s: %v", name, err)
		}
		if sent != nil {
			t.Errorf("%s: a request was sent", name)
		}
	}
}

func TestUsageMissingIsReportedAsUnreported(t *testing.T) {
	e := answersEngine(t, "data: {\"choices\":[{\"delta\":{\"content\":\"bare\"},\"index\":0}]}\ndata: [DONE]\n", nil, nil)
	res, err := e.Answer(context.Background(), AnswerRequest{Question: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Meta.CostBasis != CostUnreported || res.Meta.CostUSD != 0 || res.Citations == nil {
		t.Errorf("%+v", res)
	}
	if s := res.Meta.String(); s != "cost: $0.0000 (NOT reported by Brave) · requests: 1" {
		t.Errorf("meta line = %q", s)
	}
}
