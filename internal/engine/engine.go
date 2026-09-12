package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/nlink-jp/brave-search/internal/brave"
	"github.com/nlink-jp/brave-search/internal/config"
)

// Engine binds a configuration to an upstream client.
type Engine struct {
	Cfg    *config.Config
	Client *brave.Client
}

// New builds an engine.
func New(cfg *config.Config, client *brave.Client) *Engine {
	return &Engine{Cfg: cfg, Client: client}
}

// SearchRequestUSD is the Search plan's list price per request as published
// on 2026-09-12 ($5 per 1,000). It is an estimate: the dashboard is
// authoritative, and Answers reports its own exact cost instead.
const SearchRequestUSD = 0.005

// Cost bases, so a reader knows whether a figure is Brave's or ours.
const (
	CostEstimate   = "list_price_estimate"
	CostReported   = "reported_by_brave"
	CostUnreported = "not_reported_by_brave" // the stream carried no usage tag
)

// Meta is what every result reports beside its payload: what the call cost
// and what rate budget is left. Every call is billed and nothing is cached,
// so this is never omitted.
type Meta struct {
	Requests  int              `json:"requests"`
	Searches  int              `json:"searches,omitempty"`   // Answers: searches Brave ran
	TokensIn  int              `json:"tokens_in,omitempty"`  // Answers
	TokensOut int              `json:"tokens_out,omitempty"` // Answers
	CostUSD   float64          `json:"cost_usd"`
	CostBasis string           `json:"cost_basis"`
	RateLimit *brave.RateLimit `json:"rate_limit,omitempty"`
}

func searchMeta(m *brave.Meta) Meta {
	out := Meta{Requests: 1, CostUSD: SearchRequestUSD, CostBasis: CostEstimate}
	if m != nil {
		out.RateLimit = m.RateLimit
	}
	return out
}

// Query limits from the documentation: 1–400 characters, at most 50 words.
const (
	queryMaxChars = 400
	queryMaxWords = 50
)

var (
	countryRe   = regexp.MustCompile(`^(?i)(ALL|[A-Z]{2})$`)
	freshnessRe = regexp.MustCompile(`^(pd|pw|pm|py|\d{4}-\d{2}-\d{2}to\d{4}-\d{2}-\d{2})$`)
	langRe      = regexp.MustCompile(`^[A-Za-z]{2,}(-[A-Za-z]{2,})?$`)
)

func validateQuery(q string) error {
	q = strings.TrimSpace(q)
	if q == "" {
		return brave.Argf("query is required")
	}
	if n := len([]rune(q)); n > queryMaxChars {
		return brave.Argf("query is %d characters; Brave accepts at most %d", n, queryMaxChars)
	}
	if n := len(strings.Fields(q)); n > queryMaxWords {
		return brave.Argf("query is %d words; Brave accepts at most %d", n, queryMaxWords)
	}
	return nil
}

func validateCountry(v string) error {
	if !countryRe.MatchString(v) {
		return brave.Argf("country must be a two-letter code or ALL (got %q)", v)
	}
	return nil
}

func validateLang(name, v string) error {
	if !langRe.MatchString(v) {
		return brave.Argf("%s must be a language code such as en or ja (got %q)", name, v)
	}
	return nil
}

func validateFreshness(v string) error {
	if v != "" && !freshnessRe.MatchString(v) {
		return brave.Argf("freshness must be pd, pw, pm, py or YYYY-MM-DDtoYYYY-MM-DD (got %q)", v)
	}
	return nil
}

func validateSafesearch(v string) error {
	switch v {
	case "off", "moderate", "strict":
		return nil
	}
	return brave.Argf("safesearch must be off, moderate or strict (got %q)", v)
}

func validateRange(name string, v, lo, hi int) error {
	if v < lo || v > hi {
		return brave.Argf("%s must be between %d and %d (got %d)", name, lo, hi, v)
	}
	return nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func orDefaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// upstreamCountry normalises the code the way the documentation writes it.
func upstreamCountry(v string) string {
	return strings.ToUpper(v)
}

// String renders the meta line the CLI prints after every result.
func (m Meta) String() string {
	var b strings.Builder
	basis := "list-price estimate"
	switch m.CostBasis {
	case CostReported:
		basis = "reported by Brave"
	case CostUnreported:
		basis = "NOT reported by Brave"
	}
	fmt.Fprintf(&b, "cost: $%.4f (%s) · requests: %d", m.CostUSD, basis, m.Requests)
	if m.Searches > 0 {
		fmt.Fprintf(&b, " · searches: %d", m.Searches)
	}
	if m.TokensIn > 0 || m.TokensOut > 0 {
		fmt.Fprintf(&b, " · tokens: %d in / %d out", m.TokensIn, m.TokensOut)
	}
	if rl := m.RateLimit; rl != nil && len(rl.Remaining) > 0 {
		b.WriteString(" · rate budget left:")
		for i, rem := range rl.Remaining {
			if i > 0 {
				b.WriteString(",")
			}
			if i < len(rl.Limit) {
				fmt.Fprintf(&b, " %d/%d", rem, rl.Limit[i])
			} else {
				fmt.Fprintf(&b, " %d", rem)
			}
		}
	}
	return b.String()
}
