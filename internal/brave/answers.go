package brave

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AnswerParams drive POST /chat/completions. The call is always streamed:
// citations and research mode exist only in streaming mode, and the caller
// wants a finished answer, so the stream is assembled here.
type AnswerParams struct {
	Question   string
	Country    string
	Language   string
	Safesearch string
	MaxTokens  int // max_completion_tokens; 0 leaves it to the API

	// Research switches to research mode. Citations then arrive inside the
	// <answer> tag rather than through enable_citations, which research
	// rejects.
	Research      bool
	MaxQueries    int
	MaxIterations int
	MaxSeconds    int

	// Progress, when set, receives each complete <progress> tag as it
	// arrives, so a five-minute research call can show it is alive.
	Progress func(Progress)
}

// AnswerResult is the assembled stream.
type AnswerResult struct {
	Text         string
	Citations    []Citation
	Usage        *Usage
	Blindspots   string
	Progress     []Progress
	FinishReason string
	// Chunks counts the SSE data frames, as provenance.
	Chunks int
}

// Citation is one <citation> tag. Indexes refer to the answer text.
type Citation struct {
	Number     int    `json:"number"`
	URL        string `json:"url"`
	Snippet    string `json:"snippet,omitempty"`
	Favicon    string `json:"favicon,omitempty"`
	StartIndex int    `json:"start_index"`
	EndIndex   int    `json:"end_index"`
}

// Usage is the <usage> tag: Brave's own account of what the call cost. The
// keys are the header-style names Brave uses inside the tag.
type Usage struct {
	Requests      int     `json:"X-Request-Requests"`
	Queries       int     `json:"X-Request-Queries"`
	TokensIn      int     `json:"X-Request-Tokens-In"`
	TokensOut     int     `json:"X-Request-Tokens-Out"`
	RequestsCost  float64 `json:"X-Request-Requests-Cost"`
	QueriesCost   float64 `json:"X-Request-Queries-Cost"`
	TokensInCost  float64 `json:"X-Request-Tokens-In-Cost"`
	TokensOutCost float64 `json:"X-Request-Tokens-Out-Cost"`
	TotalCost     float64 `json:"X-Request-Total-Cost"`
}

// Progress is one <progress> tag. Its exact keys are Brave's; Fields holds
// whatever JSON it carried and Raw the text when it was not JSON.
type Progress struct {
	Fields map[string]any `json:"fields,omitempty"`
	Raw    string         `json:"raw,omitempty"`
}

// String renders a progress tag for a stderr line.
func (p Progress) String() string {
	if len(p.Fields) == 0 {
		return p.Raw
	}
	b, err := json.Marshal(p.Fields)
	if err != nil {
		return p.Raw
	}
	return string(b)
}

type chatRequest struct {
	Model              string        `json:"model"`
	Messages           []chatMessage `json:"messages"`
	Stream             bool          `json:"stream"`
	Country            string        `json:"country,omitempty"`
	Language           string        `json:"language,omitempty"`
	Safesearch         string        `json:"safesearch,omitempty"`
	MaxCompletionToken int           `json:"max_completion_tokens,omitempty"`
	EnableCitations    bool          `json:"enable_citations,omitempty"`
	EnableResearch     bool          `json:"enable_research,omitempty"`
	MaxQueries         int           `json:"research_maximum_number_of_queries,omitempty"`
	MaxIterations      int           `json:"research_maximum_number_of_iterations,omitempty"`
	MaxSeconds         int           `json:"research_maximum_number_of_seconds,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatChunk is one SSE data frame in OpenAI's chat.completion.chunk shape.
// Brave's tags travel inside delta.content.
type chatChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
		Type    string `json:"type"`
	} `json:"error"`
}

const answersEndpoint = "/chat/completions"

// Answer performs a streamed POST /chat/completions and assembles the result.
func (c *Client) Answer(ctx context.Context, p AnswerParams) (*AnswerResult, *Meta, error) {
	body := chatRequest{
		Model:              "brave",
		Messages:           []chatMessage{{Role: "user", Content: p.Question}},
		Stream:             true,
		Country:            p.Country,
		Language:           p.Language,
		Safesearch:         p.Safesearch,
		MaxCompletionToken: p.MaxTokens,
	}
	if p.Research {
		body.EnableResearch = true
		body.MaxQueries, body.MaxIterations, body.MaxSeconds = p.MaxQueries, p.MaxIterations, p.MaxSeconds
	} else {
		body.EnableCitations = true
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, nil, &Error{Code: CodeNetwork, Message: fmt.Sprintf("encode request: %v", err)}
	}

	req, err := c.newRequest(ctx, http.MethodPost, answersEndpoint, nil, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, transportError(answersEndpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	meta := &Meta{Status: resp.StatusCode, RateLimit: parseRateLimit(resp.Header)}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, meta, statusError(answersEndpoint, resp, meta.RateLimit)
	}
	res, err := readStream(resp.Body, p.Progress)
	if err != nil {
		return nil, meta, err
	}
	return res, meta, nil
}

// readStream consumes the SSE body: every `data:` frame's delta content is
// concatenated, then the tags are parsed out of the whole — a tag split
// across two frames is thereby handled without any per-frame state.
func readStream(r io.Reader, progress func(Progress)) (*AnswerResult, error) {
	sc := bufio.NewScanner(io.LimitReader(r, maxBody))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var content strings.Builder
	res := &AnswerResult{}
	emitted := 0
	done := false
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // comments, blank separators, event: lines
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			done = true
			break
		}
		var ch chatChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			return nil, decodeError(answersEndpoint, fmt.Errorf("stream frame: %w", err))
		}
		if ch.Error != nil {
			return nil, &Error{Code: CodeUpstream, Message: "Brave reported an error mid-stream: " + ch.Error.Message,
				Details: map[string]any{"endpoint": answersEndpoint, "upstream_code": ch.Error.Code, "upstream_type": ch.Error.Type}}
		}
		res.Chunks++
		for _, choice := range ch.Choices {
			content.WriteString(choice.Delta.Content)
			if choice.FinishReason != "" {
				res.FinishReason = choice.FinishReason
			}
		}
		if progress != nil {
			for _, inner := range completeJSONTags(content.String(), "progress")[emitted:] {
				emitted++
				progress(parseProgress(inner))
			}
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, decodeError(answersEndpoint, fmt.Errorf("a stream line exceeded 4 MB"))
		}
		return nil, transportError(answersEndpoint, err)
	}
	if res.Chunks == 0 {
		return nil, decodeError(answersEndpoint, fmt.Errorf("the stream carried no data frames"))
	}
	if !done {
		// Brave ends every stream with [DONE] (measured 2026-09-12). Without
		// it the body was cut — by the 16 MB cap, a dropped connection, or
		// upstream — and a partial answer must not pass as a complete one.
		return nil, &Error{Code: CodeUpstream, Message: "the answer stream ended before [DONE]; the answer is incomplete",
			Details: map[string]any{"endpoint": answersEndpoint, "chunks": res.Chunks}}
	}
	assemble(res, content.String())
	return res, nil
}

// Tags Brave embeds in the stream. Debug tags are stripped and discarded.
var (
	debugTags = []string{"queries", "analyzing", "thinking"}
)

// assemble parses the concatenated content into the result.
//
// Brave's protocol has no escaping: a tag name that appears literally in an
// answer ("how do HTML <progress> elements work") is indistinguishable from
// a control tag by its brackets alone. The JSON-carrying tags (citation,
// usage, progress) are therefore honoured only when their body parses as a
// JSON object — literal text falls through untouched. The text-carrying tags
// (answer, blindspots, and the debug tags) cannot be told apart, and are
// stripped as Brave's; that limit is recorded in AGENTS.md.
func assemble(res *AnswerResult, content string) {
	// Usage: the last one wins, as an unbounded stream could carry several.
	for _, inner := range completeJSONTags(content, "usage") {
		var u Usage
		if json.Unmarshal([]byte(inner), &u) == nil {
			res.Usage = &u
		}
	}
	for _, inner := range completeJSONTags(content, "progress") {
		res.Progress = append(res.Progress, parseProgress(inner))
	}
	if bs := completeTags(content, "blindspots"); len(bs) > 0 {
		res.Blindspots = strings.TrimSpace(strings.Join(bs, "\n"))
	}

	// The answer body: the first <answer> tag in research mode, else
	// everything that is not a tag. Only trailing whitespace is trimmed, so
	// Brave's citation indexes keep their meaning relative to the text.
	text := content
	if answers := completeTags(content, "answer"); len(answers) > 0 {
		text = answers[0]
	}
	for _, inner := range completeJSONTags(text, "citation") {
		var c Citation
		if json.Unmarshal([]byte(inner), &c) == nil && c.URL != "" {
			res.Citations = append(res.Citations, c)
		}
	}
	text = stripJSONTags(text, "citation")
	text = stripJSONTags(text, "usage")
	text = stripJSONTags(text, "progress")
	for _, name := range append([]string{"blindspots", "answer"}, debugTags...) {
		text = stripTags(text, name)
	}
	res.Text = strings.TrimRight(text, " \t\r\n")
}

// isJSONObject reports whether s is a JSON object, which is what every
// machine tag Brave emits carries. Prose that merely sits between literal
// brackets is not.
func isJSONObject(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		return false
	}
	var m map[string]json.RawMessage
	return json.Unmarshal([]byte(s), &m) == nil
}

// jsonTags returns every <name>…</name> pair whose body is a JSON object, as
// (start, end) offsets of the whole tag plus the body. A literal "<name>" in
// prose is skipped by one tag, not by one pair: otherwise "a <progress>
// element … <progress>{json}</progress>" would pair the literal opener with
// the real closer and lose the real tag.
func jsonTags(s, name string) (spans [][2]int, bodies []string) {
	open, close := "<"+name+">", "</"+name+">"
	pos := 0
	for {
		i := strings.Index(s[pos:], open)
		if i < 0 {
			return spans, bodies
		}
		i += pos
		bodyStart := i + len(open)
		j := strings.Index(s[bodyStart:], close)
		if j < 0 {
			return spans, bodies
		}
		body := s[bodyStart : bodyStart+j]
		if !isJSONObject(body) {
			pos = bodyStart // a literal opener: look for the next one inside
			continue
		}
		end := bodyStart + j + len(close)
		spans = append(spans, [2]int{i, end})
		bodies = append(bodies, body)
		pos = end
	}
}

// completeJSONTags returns the bodies of every JSON-carrying tag.
func completeJSONTags(s, name string) []string {
	_, bodies := jsonTags(s, name)
	return bodies
}

// stripJSONTags removes every JSON-carrying <name>…</name> pair and leaves
// any literal occurrence in place.
func stripJSONTags(s, name string) string {
	spans, _ := jsonTags(s, name)
	if len(spans) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(s[last:sp[0]])
		last = sp[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

func parseProgress(inner string) Progress {
	var fields map[string]any
	if json.Unmarshal([]byte(inner), &fields) == nil && fields != nil {
		return Progress{Fields: fields}
	}
	return Progress{Raw: strings.TrimSpace(inner)}
}

// AnswersEndpoint is exported for callers that key on the endpoint.
const AnswersEndpoint = answersEndpoint

// completeTags returns the inner text of every complete <name>…</name> pair,
// in order. An unterminated tag is left alone (it may still be arriving).
func completeTags(s, name string) []string {
	open, close := "<"+name+">", "</"+name+">"
	var out []string
	for {
		i := strings.Index(s, open)
		if i < 0 {
			return out
		}
		rest := s[i+len(open):]
		j := strings.Index(rest, close)
		if j < 0 {
			return out
		}
		out = append(out, rest[:j])
		s = rest[j+len(close):]
	}
}

// stripTags removes every complete <name>…</name> pair, tag and content.
func stripTags(s, name string) string {
	open, close := "<"+name+">", "</"+name+">"
	var b strings.Builder
	for {
		i := strings.Index(s, open)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		rest := s[i+len(open):]
		j := strings.Index(rest, close)
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		s = rest[j+len(close):]
	}
}
