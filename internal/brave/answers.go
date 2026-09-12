package brave

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
			for _, inner := range completeTags(content.String(), "progress")[emitted:] {
				progress(parseProgress(inner))
				emitted++
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, transportError(answersEndpoint, err)
	}
	if res.Chunks == 0 {
		return nil, decodeError(answersEndpoint, fmt.Errorf("the stream carried no data frames"))
	}
	assemble(res, content.String())
	return res, nil
}

// Tags Brave embeds in the stream. Debug tags are stripped and discarded.
var (
	debugTags = []string{"queries", "analyzing", "thinking"}
)

// assemble parses the concatenated content into the result.
func assemble(res *AnswerResult, content string) {
	// Usage: the last one wins, as an unbounded stream could carry several.
	for _, inner := range completeTags(content, "usage") {
		var u Usage
		if json.Unmarshal([]byte(inner), &u) == nil {
			res.Usage = &u
		}
	}
	for _, inner := range completeTags(content, "progress") {
		res.Progress = append(res.Progress, parseProgress(inner))
	}
	if bs := completeTags(content, "blindspots"); len(bs) > 0 {
		res.Blindspots = strings.TrimSpace(strings.Join(bs, "\n"))
	}

	// The answer body: the <answer> tag in research mode, else everything
	// that is not a tag.
	text := content
	if answers := completeTags(content, "answer"); len(answers) > 0 {
		text = strings.Join(answers, "\n")
	}
	for _, inner := range completeTags(text, "citation") {
		var c Citation
		if json.Unmarshal([]byte(inner), &c) == nil {
			res.Citations = append(res.Citations, c)
		}
	}
	for _, name := range append([]string{"citation", "usage", "progress", "blindspots", "answer"}, debugTags...) {
		text = stripTags(text, name)
	}
	res.Text = strings.TrimSpace(text)
}

func parseProgress(inner string) Progress {
	var fields map[string]any
	if json.Unmarshal([]byte(inner), &fields) == nil && fields != nil {
		return Progress{Fields: fields}
	}
	return Progress{Raw: strings.TrimSpace(inner)}
}

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
