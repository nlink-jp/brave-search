package engine

import (
	"context"
	"strings"
	"time"

	"github.com/nlink-jp/brave-search/internal/brave"
	"github.com/nlink-jp/brave-search/internal/config"
)

// AnswerRequest is a single-search Answers call. Zero values take the
// configured defaults.
type AnswerRequest struct {
	Question   string
	Country    string
	Language   string
	Safesearch string
	MaxTokens  int
}

// ResearchRequest is a research-mode Answers call.
type ResearchRequest struct {
	Question      string
	Country       string
	Language      string
	Safesearch    string
	MaxQueries    int
	MaxIterations int
	MaxSeconds    int
	// Timeout, when set, replaces the deadline derived from MaxSeconds. It
	// exists so an explicit --timeout is honoured rather than dropped.
	Timeout time.Duration
}

// AnswerResult is the stable shape of both Answers modes.
type AnswerResult struct {
	Question   string           `json:"question"`
	Mode       string           `json:"mode"` // "answer" | "research"
	Answer     string           `json:"answer"`
	Citations  []brave.Citation `json:"citations"`
	Blindspots string           `json:"blindspots,omitempty"`
	Progress   []brave.Progress `json:"progress,omitempty"`
	// Note is present only when something about the result needs saying —
	// today, that Brave returned no citations for a non-English reply.
	Note string `json:"note,omitempty"`
	Meta Meta   `json:"meta"`
}

// NoCitationsNonEnglish is the note attached when a reply in a language other
// than English came back without citations. Measured 2026-09-12 on one
// question: language=en carried citations in 2 of 2 runs (29, 29); language=ja
// in 1 of 3 (0, 0, 24), country held constant. Not deterministic, but the
// failures clustered on the non-English replies.
const NoCitationsNonEnglish = "Brave returned no citations. Non-English replies were observed to come back " +
	"without them intermittently (language=ja: none in 2 of 3 runs on 2026-09-12; English: every run had them). " +
	"Ask again, or ask in English, if sources matter."

// Answer validates, fills defaults, and performs one single-search call.
func (e *Engine) Answer(ctx context.Context, r AnswerRequest) (*AnswerResult, error) {
	a := e.Cfg.Answers
	p := brave.AnswerParams{
		Question:   r.Question,
		Country:    upstreamCountry(orDefault(r.Country, a.Country)),
		Language:   orDefault(r.Language, a.Language),
		Safesearch: orDefault(r.Safesearch, a.Safesearch),
		MaxTokens:  orDefaultInt(r.MaxTokens, a.MaxTokens),
	}
	if err := validateAnswerCommon(p); err != nil {
		return nil, err
	}
	if p.MaxTokens < 0 {
		return nil, brave.Argf("max_tokens cannot be negative (got %d)", p.MaxTokens)
	}
	res, meta, err := e.Client.Answer(ctx, p)
	if err != nil {
		return nil, err
	}
	return shapeAnswer(p.Question, "answer", p.Language, res, meta), nil
}

// Research validates, fills defaults, and performs one research-mode call.
// progress, when set, receives each progress report as it arrives.
func (e *Engine) Research(ctx context.Context, r ResearchRequest, progress func(brave.Progress)) (*AnswerResult, error) {
	a := e.Cfg.Answers
	p := brave.AnswerParams{
		Question:      r.Question,
		Country:       upstreamCountry(orDefault(r.Country, a.Country)),
		Language:      orDefault(r.Language, a.Language),
		Safesearch:    orDefault(r.Safesearch, a.Safesearch),
		Research:      true,
		MaxQueries:    orDefaultInt(r.MaxQueries, a.ResearchMaxQueries),
		MaxIterations: orDefaultInt(r.MaxIterations, a.ResearchMaxIterations),
		MaxSeconds:    orDefaultInt(r.MaxSeconds, a.ResearchMaxSeconds),
		Progress:      progress,
	}
	if err := validateAnswerCommon(p); err != nil {
		return nil, err
	}
	if err := validateRange("max_queries", p.MaxQueries, 1, config.MaxQueriesMax); err != nil {
		return nil, err
	}
	if err := validateRange("max_iterations", p.MaxIterations, 1, config.MaxItersMax); err != nil {
		return nil, err
	}
	if err := validateRange("max_seconds", p.MaxSeconds, 1, config.MaxSecondsMax); err != nil {
		return nil, err
	}
	// The HTTP deadline follows the time budget rather than the shared
	// timeout, so a 300-second research call is not cut off at 30 — unless
	// the caller set one explicitly.
	deadline := e.Cfg.ResearchTimeout(p.MaxSeconds)
	if r.Timeout > 0 {
		deadline = r.Timeout
	}
	client := e.Client.WithTimeout(deadline)
	res, meta, err := client.Answer(ctx, p)
	if err != nil {
		return nil, err
	}
	return shapeAnswer(p.Question, "research", p.Language, res, meta), nil
}

// validateAnswerCommon checks what both Answers modes share. The 400-char /
// 50-word limit is a Web Search rule and is not applied here: a research
// brief is allowed to be long, and the Answers documentation states no limit.
func validateAnswerCommon(p brave.AnswerParams) error {
	if strings.TrimSpace(p.Question) == "" {
		return brave.Argf("question is required")
	}
	if err := validateCountry(p.Country); err != nil {
		return err
	}
	if err := validateLang("language", p.Language); err != nil {
		return err
	}
	return validateSafesearch(p.Safesearch)
}

func shapeAnswer(question, mode, language string, res *brave.AnswerResult, meta *brave.Meta) *AnswerResult {
	out := &AnswerResult{
		Question:   question,
		Mode:       mode,
		Answer:     res.Text,
		Citations:  res.Citations,
		Blindspots: res.Blindspots,
		Progress:   res.Progress,
		Meta:       Meta{Requests: 1, CostBasis: CostUnreported},
	}
	if out.Citations == nil {
		out.Citations = []brave.Citation{}
	}
	if len(out.Citations) == 0 && !strings.EqualFold(language, "en") && res.Text != "" {
		out.Note = NoCitationsNonEnglish
	}
	if u := res.Usage; u != nil {
		out.Meta.CostBasis = CostReported
		out.Meta.CostUSD = u.TotalCost
		out.Meta.Searches = u.Queries
		out.Meta.TokensIn, out.Meta.TokensOut = u.TokensIn, u.TokensOut
		if u.Requests > 0 {
			out.Meta.Requests = u.Requests
		}
	}
	if meta != nil {
		out.Meta.RateLimit = meta.RateLimit
	}
	return out
}
