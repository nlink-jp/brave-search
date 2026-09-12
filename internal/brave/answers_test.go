package brave

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Synthetic SSE streams. The frame shape is OpenAI's chat.completion.chunk
// and the tag names are Brave's documented ones; every word of content is
// invented. A live stream is never stored here (Brave ToS).

const singleStream = `: keep-alive comment
event: message
data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant","content":"Generics let "},"index":0}]}

data: {"choices":[{"delta":{"content":"you write typed code.<cita"},"index":0}]}
data: {"choices":[{"delta":{"content":"tion>{\"start_index\":0,\"end_index\":21,\"number\":1,\"url\":\"https://example.com/g\",\"snippet\":\"typed code\",\"favicon\":\"https://example.com/f.png\"}</citation>"},"index":0}]}
data: {"choices":[{"delta":{"content":"<usage>{\"X-Request-Requests\":1,\"X-Request-Queries\":1,\"X-Request-Tokens-In\":100,\"X-Request-Tokens-Out\":20,\"X-Request-Queries-Cost\":0.004,\"X-Request-Tokens-In-Cost\":0.0005,\"X-Request-Tokens-Out-Cost\":0.0001,\"X-Request-Total-Cost\":0.0046}</usage>"},"index":0,"finish_reason":"stop"}]}
data: [DONE]
`

const researchStream = `data: {"choices":[{"delta":{"content":"<queries>[\"q one\",\"q two\"]</queries>"},"index":0}]}
data: {"choices":[{"delta":{"content":"<progress>{\"iteration\":1,\"queries\":2,\"urls\":8,\"elapsed_seconds\":4}</progress>"},"index":0}]}
data: {"choices":[{"delta":{"content":"<thinking>choosing urls</thinking><analyzing>{\"urls\":8}</analyzing>"},"index":0}]}
data: {"choices":[{"delta":{"content":"<progress>{\"iteration\":2,\"queries\":4,\"urls\":15,\"elapsed_seconds\":9}</progress>"},"index":0}]}
data: {"choices":[{"delta":{"content":"<blindspots>No source covered pricing after 2026.</blindspots>"},"index":0}]}
data: {"choices":[{"delta":{"content":"<answer>Alpha does X.<citation>{\"number\":1,\"url\":\"https://example.com/a\"}</citation> Beta does Y.<citation>{\"number\":2,\"url\":\"https://example.com/b\"}</citation></answer>"},"index":0}]}
data: {"choices":[{"delta":{"content":"<usage>{\"X-Request-Requests\":1,\"X-Request-Queries\":4,\"X-Request-Tokens-In\":3000,\"X-Request-Tokens-Out\":200,\"X-Request-Total-Cost\":0.032}</usage>"},"index":0,"finish_reason":"stop"}]}
data: [DONE]
`

func serveStream(t *testing.T, stream string) (*Client, *capture, *[]byte) {
	t.Helper()
	cap := &capture{}
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.method, cap.path, cap.header = r.Method, r.URL.Path, r.Header.Clone()
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-RateLimit-Remaining", "1, 500")
		_, _ = w.Write([]byte(stream))
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "search-key", "", 5*time.Second, "t")
	c.AnswersAPIKey = "answers-key"
	return c, cap, &body
}

func TestAnswerSingleSearch(t *testing.T) {
	c, cap, body := serveStream(t, singleStream)
	res, meta, err := c.Answer(context.Background(), AnswerParams{
		Question: "what are generics", Country: "US", Language: "en", Safesearch: "moderate", MaxTokens: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.method != "POST" || cap.path != "/chat/completions" || cap.header.Get("Content-Type") != "application/json" {
		t.Errorf("%s %s %s", cap.method, cap.path, cap.header.Get("Content-Type"))
	}
	var sent map[string]any
	if err := json.Unmarshal(*body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent["model"] != "brave" || sent["stream"] != true || sent["enable_citations"] != true || sent["max_completion_tokens"] != float64(300) {
		t.Errorf("request = %v", sent)
	}
	if _, ok := sent["enable_research"]; ok {
		t.Error("enable_research was sent for a single search")
	}
	msgs := sent["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["content"] != "what are generics" {
		t.Errorf("messages = %v", msgs)
	}

	if res.Text != "Generics let you write typed code." {
		t.Errorf("text = %q", res.Text)
	}
	if len(res.Citations) != 1 || res.Citations[0].URL != "https://example.com/g" || res.Citations[0].EndIndex != 21 {
		t.Errorf("citations = %+v", res.Citations)
	}
	if res.Usage == nil || res.Usage.TotalCost != 0.0046 || res.Usage.Queries != 1 || res.Usage.TokensIn != 100 {
		t.Errorf("usage = %+v", res.Usage)
	}
	if res.FinishReason != "stop" || res.Chunks != 4 {
		t.Errorf("finish=%q chunks=%d", res.FinishReason, res.Chunks)
	}
	if meta.RateLimit == nil || meta.RateLimit.Remaining[1] != 500 {
		t.Errorf("meta = %+v", meta)
	}
}

func TestAnswerResearchMode(t *testing.T) {
	c, _, body := serveStream(t, researchStream)
	var seen []Progress
	res, _, err := c.Answer(context.Background(), AnswerParams{
		Question: "compare alpha and beta", Research: true, MaxQueries: 3, MaxIterations: 2, MaxSeconds: 60,
		Progress: func(p Progress) { seen = append(seen, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_ = json.Unmarshal(*body, &sent)
	if sent["enable_research"] != true || sent["research_maximum_number_of_queries"] != float64(3) ||
		sent["research_maximum_number_of_iterations"] != float64(2) || sent["research_maximum_number_of_seconds"] != float64(60) {
		t.Errorf("request = %v", sent)
	}
	if _, ok := sent["enable_citations"]; ok {
		t.Error("enable_citations was sent in research mode (Brave rejects the combination)")
	}
	if res.Text != "Alpha does X. Beta does Y." {
		t.Errorf("text = %q", res.Text)
	}
	if len(res.Citations) != 2 || res.Citations[1].Number != 2 {
		t.Errorf("citations = %+v", res.Citations)
	}
	if res.Blindspots != "No source covered pricing after 2026." {
		t.Errorf("blindspots = %q", res.Blindspots)
	}
	if len(res.Progress) != 2 || res.Progress[1].Fields["iteration"] != float64(2) {
		t.Errorf("progress = %+v", res.Progress)
	}
	if len(seen) != 2 || seen[0].Fields["urls"] != float64(8) {
		t.Errorf("progress callback saw %+v", seen)
	}
	if res.Usage == nil || res.Usage.Queries != 4 {
		t.Errorf("usage = %+v", res.Usage)
	}
	for _, leaked := range []string{"<thinking>", "<queries>", "<analyzing>", "choosing urls", "q one"} {
		if strings.Contains(res.Text, leaked) {
			t.Errorf("debug content leaked into the answer: %q", leaked)
		}
	}
}

// Brave issues one key per plan, so the Search key is never sent to
// Answers: without an Answers key nothing is sent at all.
func TestAnswersNeedsItsOwnKey(t *testing.T) {
	c, cap, _ := serveStream(t, singleStream)
	c.AnswersAPIKey = ""
	_, _, err := c.Answer(context.Background(), AnswerParams{Question: "q"})
	if Code(err) != CodeMissingAPIKey || !strings.Contains(err.Error(), "answers_api_key") {
		t.Fatalf("err = %v", err)
	}
	if cap.method != "" {
		t.Error("a request was sent without an Answers key")
	}
	c.AnswersAPIKey = "answers-key"
	if _, _, err := c.Answer(context.Background(), AnswerParams{Question: "q"}); err != nil {
		t.Fatal(err)
	}
	if cap.header.Get("X-Subscription-Token") != "answers-key" {
		t.Errorf("token = %q", cap.header.Get("X-Subscription-Token"))
	}
	if _, _, err := c.WebSearch(context.Background(), WebParams{Query: "q"}); err == nil && cap.header.Get("X-Subscription-Token") != "search-key" {
		t.Errorf("the answers key leaked into a search call: %q", cap.header.Get("X-Subscription-Token"))
	}
}

func TestStreamErrorsAreSurfaced(t *testing.T) {
	for name, tc := range map[string]struct{ stream, code string }{
		"mid-stream error": {`data: {"error":{"message":"quota exhausted","code":"QUOTA","type":"billing"}}` + "\n", CodeUpstream},
		"bad frame":        {"data: {not json}\n", CodeDecode},
		"empty":            {"\n\n", CodeDecode},
		"only done":        {"data: [DONE]\n", CodeDecode},
		"cut before DONE":  {"data: {\"choices\":[{\"delta\":{\"content\":\"half an ans\"},\"index\":0}]}\n", CodeUpstream},
	} {
		c, _, _ := serveStream(t, tc.stream)
		_, _, err := c.Answer(context.Background(), AnswerParams{Question: "q"})
		if Code(err) != tc.code {
			t.Errorf("%s: code = %s, want %s (%v)", name, Code(err), tc.code, err)
		}
	}
}

func TestAnswerStatusErrorsMapLikeSearch(t *testing.T) {
	c, _ := serve(t, 403, `{"error":{"code":"SUBSCRIPTION_PLAN","detail":"answers not subscribed"}}`, nil)
	c.AnswersAPIKey = "answers-key"
	_, _, err := c.Answer(context.Background(), AnswerParams{Question: "q"})
	if Code(err) != CodePlanNotSubscribed || !strings.Contains(err.Error(), "answers not subscribed") {
		t.Errorf("err = %v", err)
	}
}

// Brave's tags have no escaping, so literal tag text in an answer must not be
// mistaken for a control tag when its body is not a JSON object; a citation
// whose snippet holds a closing tag is dropped rather than mis-parsed.
func TestLiteralTagTextSurvives(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"HTML has a <progress> element; write <progress value=1> in markup.\"},\"index\":0}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" Models emit <thinking>steps</thinking> too.<citation>{\\\"number\\\":1,\\\"url\\\":\\\"https://example.com/p\\\"}</citation><citation>not json</citation><progress>{\\\"iteration\\\":1}</progress>\"},\"index\":0}]}\n" +
		"data: [DONE]\n"
	c, _, _ := serveStream(t, stream)
	var reports int
	res, _, err := c.Answer(context.Background(), AnswerParams{Question: "q", Progress: func(Progress) { reports++ }})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "<progress> element") || !strings.Contains(res.Text, "<progress value=1>") {
		t.Errorf("literal <progress> text was damaged: %q", res.Text)
	}
	if !strings.Contains(res.Text, "<citation>not json</citation>") {
		t.Errorf("a non-JSON <citation> was stripped: %q", res.Text)
	}
	if strings.Contains(res.Text, "example.com/p") || strings.Contains(res.Text, `"iteration"`) {
		t.Errorf("a JSON tag leaked into the text: %q", res.Text)
	}
	if len(res.Citations) != 1 || len(res.Progress) != 1 || reports != 1 {
		t.Errorf("citations=%d progress=%d reports=%d", len(res.Citations), len(res.Progress), reports)
	}
	// The text-carrying debug tag cannot be told apart and is stripped —
	// the documented limit.
	if strings.Contains(res.Text, "steps") {
		t.Errorf("expected the literal <thinking> text to be stripped (documented limit): %q", res.Text)
	}
}

// SSE framing variants: data without a space, CRLF line endings, a frame
// with several choices, and comment/event lines between frames.
func TestStreamFramingVariants(t *testing.T) {
	stream := ": comment\r\n" +
		"event: message\r\n" +
		"data:{\"choices\":[{\"delta\":{\"content\":\"one \"},\"index\":0},{\"delta\":{\"content\":\"two\"},\"index\":1}]}\r\n" +
		"\r\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" three\"},\"index\":0,\"finish_reason\":\"stop\"}]}\r\n" +
		"data: [DONE]\r\n"
	c, _, _ := serveStream(t, stream)
	res, _, err := c.Answer(context.Background(), AnswerParams{Question: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "one two three" || res.Chunks != 2 || res.FinishReason != "stop" {
		t.Errorf("text=%q chunks=%d finish=%q", res.Text, res.Chunks, res.FinishReason)
	}
}

func TestLeadingWhitespaceIsKeptForCitationIndexes(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"<answer>\\n\\nText.<citation>{\\\"number\\\":1,\\\"url\\\":\\\"https://example.com\\\",\\\"start_index\\\":7,\\\"end_index\\\":7}</citation>\\n</answer>\"},\"index\":0}]}\n" +
		"data: [DONE]\n"
	c, _, _ := serveStream(t, stream)
	res, _, err := c.Answer(context.Background(), AnswerParams{Question: "q", Research: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "\n\nText." {
		t.Errorf("text = %q (leading whitespace must survive, trailing must go)", res.Text)
	}
}

func TestUsageAbsentIsNotAnError(t *testing.T) {
	c, _, _ := serveStream(t, "data: {\"choices\":[{\"delta\":{\"content\":\"plain answer\"},\"index\":0}]}\ndata: [DONE]\n")
	res, _, err := c.Answer(context.Background(), AnswerParams{Question: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage != nil || res.Text != "plain answer" || len(res.Citations) != 0 {
		t.Errorf("%+v", res)
	}
}

func TestJSONTagHelpers(t *testing.T) {
	s := `a <x> literal <x>{"k":1}</x> b <x>{"k":2}</x> <x>tail`
	if got := completeJSONTags(s, "x"); len(got) != 2 || got[0] != `{"k":1}` || got[1] != `{"k":2}` {
		t.Errorf("completeJSONTags = %v", got)
	}
	if got := stripJSONTags(s, "x"); got != "a <x> literal  b  <x>tail" {
		t.Errorf("stripJSONTags = %q", got)
	}
}

func TestTagHelpers(t *testing.T) {
	s := "a<x>1</x>b<x>2</x>c<x>unterminated"
	if got := completeTags(s, "x"); len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Errorf("completeTags = %v", got)
	}
	if got := stripTags(s, "x"); got != "abc<x>unterminated" {
		t.Errorf("stripTags = %q", got)
	}
	if p := parseProgress("not json"); p.Raw != "not json" || p.String() != "not json" {
		t.Errorf("parseProgress = %+v", p)
	}
	if p := parseProgress(`{"k":1}`); p.Fields["k"] != float64(1) || p.String() != `{"k":1}` {
		t.Errorf("parseProgress = %+v", p)
	}
}
