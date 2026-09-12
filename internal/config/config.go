package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Built-in defaults. Country and language follow Brave's own defaults rather
// than the operator's locale, so a fresh install behaves exactly like the API
// documentation says; a Japanese operator sets "JP" / "ja" in the config.
const (
	DefaultBaseURL     = "https://api.search.brave.com/res/v1"
	DefaultTimeout     = 30 * time.Second
	DefaultCountry     = "US"
	DefaultSearchLang  = "en"
	DefaultLanguage    = "en"
	DefaultSafesearch  = "moderate"
	DefaultCount       = 10
	DefaultMaxTokens   = 8192
	DefaultMaxURLs     = 20
	DefaultMaxQueries  = 10
	DefaultMaxIters    = 2
	DefaultMaxSeconds  = 120
	ResearchTimeoutPad = 30 * time.Second // added to research_max_seconds for the HTTP deadline
)

// Upstream ranges, validated locally so a bad value fails before a request is
// spent. Source: the Brave API documentation as of 2026-09-12; re-verify.
const (
	WebCountMax     = 20
	ContextCountMax = 50
	OffsetMax       = 9
	MaxTokensMin    = 1024
	MaxTokensMax    = 32768
	MaxURLsMax      = 50
	MaxQueriesMax   = 50
	MaxItersMax     = 5
	MaxSecondsMax   = 300
)

// Search holds the defaults for the web and context commands.
type Search struct {
	Country    string
	SearchLang string
	Safesearch string
	Count      int
}

// Context holds the defaults specific to the LLM Context endpoint.
type Context struct {
	MaxTokens int
	MaxURLs   int
}

// Answers holds the defaults for the answer and research commands.
type Answers struct {
	Country               string
	Language              string
	Safesearch            string
	MaxTokens             int // 0 = leave to the API
	ResearchMaxQueries    int
	ResearchMaxIterations int
	ResearchMaxSeconds    int
}

// Config holds resolved runtime settings.
type Config struct {
	APIKey        string
	AnswersAPIKey string // the Answers plan has its own key; sent to /chat/completions
	BaseURL       string
	APIVersion    string // Api-Version header; "" sends none (latest)
	Timeout       time.Duration
	Search        Search
	Context       Context
	Answers       Answers

	// Path is the config file that was actually read, or "" when none was.
	// Reported by `auth check` so a file that exists but is not being read is
	// diagnosable at a glance.
	Path string
}

// Defaults returns the built-in configuration.
func Defaults() *Config {
	return &Config{
		BaseURL: DefaultBaseURL,
		Timeout: DefaultTimeout,
		Search:  Search{Country: DefaultCountry, SearchLang: DefaultSearchLang, Safesearch: DefaultSafesearch, Count: DefaultCount},
		Context: Context{MaxTokens: DefaultMaxTokens, MaxURLs: DefaultMaxURLs},
		Answers: Answers{
			Country: DefaultCountry, Language: DefaultLanguage, Safesearch: DefaultSafesearch,
			ResearchMaxQueries: DefaultMaxQueries, ResearchMaxIterations: DefaultMaxIters, ResearchMaxSeconds: DefaultMaxSeconds,
		},
	}
}

// Load resolves configuration. If configPath is empty the first existing file
// among SearchPaths is used. Environment variables override file values; a
// non-zero timeoutOverride wins over both.
func Load(configPath string, timeoutOverride time.Duration) (*Config, error) {
	cfg := Defaults()

	if configPath == "" {
		configPath = FindConfig()
	}
	if configPath != "" {
		f, err := os.Open(configPath)
		switch {
		case err == nil:
			sections, perr := parseTOML(f)
			_ = f.Close()
			if perr != nil {
				return nil, fmt.Errorf("parse config %s: %w", configPath, perr)
			}
			if aerr := applySections(cfg, sections); aerr != nil {
				return nil, fmt.Errorf("config %s: %w", configPath, aerr)
			}
			cfg.Path = configPath
		case os.IsNotExist(err):
			// An explicit --config that does not exist is a mistake worth
			// reporting; a default path that does not exist is normal.
			if !isSearchPath(configPath) {
				return nil, fmt.Errorf("config %s: %w", configPath, err)
			}
		default:
			return nil, fmt.Errorf("open config %s: %w", configPath, err)
		}
	}

	if err := applyEnv(cfg); err != nil {
		return nil, err
	}
	if timeoutOverride > 0 {
		cfg.Timeout = timeoutOverride
	}
	return cfg, Validate(cfg)
}

// HasKey reports whether an API key is configured.
func (c *Config) HasKey() bool { return c.APIKey != "" }

// ResearchTimeout is the HTTP deadline for a research call: the upstream
// budget plus a grace period, derived rather than configured so the two can
// never disagree.
func (c *Config) ResearchTimeout(maxSeconds int) time.Duration {
	if maxSeconds <= 0 {
		maxSeconds = c.Answers.ResearchMaxSeconds
	}
	return time.Duration(maxSeconds)*time.Second + ResearchTimeoutPad
}

// Redacted returns the config with the key replaced by a fixed marker, for
// anything that prints or serialises settings. The key is never rendered.
func (c *Config) Redacted() Config {
	clone := *c
	if clone.APIKey != "" {
		clone.APIKey = "[set]"
	}
	if clone.AnswersAPIKey != "" {
		clone.AnswersAPIKey = "[set]"
	}
	return clone
}

var safesearchValues = map[string]bool{"off": true, "moderate": true, "strict": true}

// Validate checks every range the upstream documents, so a bad config or a bad
// flag fails locally instead of spending a request on a 422.
func Validate(cfg *Config) error {
	if cfg.BaseURL == "" {
		return fmt.Errorf("[api] base_url must not be empty")
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("[api] timeout must be positive")
	}
	if err := checkRange("[search] count", cfg.Search.Count, 1, WebCountMax); err != nil {
		return err
	}
	if err := checkSafesearch("[search] safesearch", cfg.Search.Safesearch); err != nil {
		return err
	}
	if err := checkSafesearch("[answers] safesearch", cfg.Answers.Safesearch); err != nil {
		return err
	}
	if err := checkRange("[context] max_tokens", cfg.Context.MaxTokens, MaxTokensMin, MaxTokensMax); err != nil {
		return err
	}
	if err := checkRange("[context] max_urls", cfg.Context.MaxURLs, 1, MaxURLsMax); err != nil {
		return err
	}
	if cfg.Answers.MaxTokens < 0 {
		return fmt.Errorf("[answers] max_tokens cannot be negative")
	}
	if err := checkRange("[answers] research_max_queries", cfg.Answers.ResearchMaxQueries, 1, MaxQueriesMax); err != nil {
		return err
	}
	if err := checkRange("[answers] research_max_iterations", cfg.Answers.ResearchMaxIterations, 1, MaxItersMax); err != nil {
		return err
	}
	if err := checkRange("[answers] research_max_seconds", cfg.Answers.ResearchMaxSeconds, 1, MaxSecondsMax); err != nil {
		return err
	}
	return nil
}

func checkRange(name string, v, lo, hi int) error {
	if v < lo || v > hi {
		return fmt.Errorf("%s must be between %d and %d (got %d)", name, lo, hi, v)
	}
	return nil
}

func checkSafesearch(name, v string) error {
	if !safesearchValues[v] {
		return fmt.Errorf("%s must be off, moderate or strict (got %q)", name, v)
	}
	return nil
}

// knownKeys is the whole vocabulary of the file. A key outside it is a typo,
// and a typo that is silently ignored is the worst kind of configuration bug.
var knownKeys = map[string]map[string]bool{
	"api":     {"api_key": true, "answers_api_key": true, "base_url": true, "api_version": true, "timeout": true},
	"search":  {"country": true, "search_lang": true, "safesearch": true, "count": true},
	"context": {"max_tokens": true, "max_urls": true},
	"answers": {
		"country": true, "language": true, "safesearch": true, "max_tokens": true,
		"research_max_queries": true, "research_max_iterations": true, "research_max_seconds": true,
	},
}

func applySections(cfg *Config, sections map[string]map[string]string) error {
	for sec, kv := range sections {
		if sec == "" {
			if len(kv) > 0 {
				return fmt.Errorf("keys outside a [section] are not allowed")
			}
			continue
		}
		known, ok := knownKeys[sec]
		if !ok {
			return fmt.Errorf("unknown section [%s]", sec)
		}
		for k := range kv {
			if !known[k] {
				return fmt.Errorf("unknown key [%s] %s", sec, k)
			}
		}
	}
	var err error
	if a := sections["api"]; a != nil {
		setString(&cfg.APIKey, a["api_key"])
		setString(&cfg.AnswersAPIKey, a["answers_api_key"])
		setString(&cfg.BaseURL, a["base_url"])
		setString(&cfg.APIVersion, a["api_version"])
		if v := a["timeout"]; v != "" {
			if cfg.Timeout, err = parseDuration(v); err != nil {
				return fmt.Errorf("[api] timeout: %w", err)
			}
		}
	}
	if s := sections["search"]; s != nil {
		setString(&cfg.Search.Country, s["country"])
		setString(&cfg.Search.SearchLang, s["search_lang"])
		setString(&cfg.Search.Safesearch, s["safesearch"])
		if err = setInt(&cfg.Search.Count, s["count"], "[search] count"); err != nil {
			return err
		}
	}
	if c := sections["context"]; c != nil {
		if err = setInt(&cfg.Context.MaxTokens, c["max_tokens"], "[context] max_tokens"); err != nil {
			return err
		}
		if err = setInt(&cfg.Context.MaxURLs, c["max_urls"], "[context] max_urls"); err != nil {
			return err
		}
	}
	if a := sections["answers"]; a != nil {
		setString(&cfg.Answers.Country, a["country"])
		setString(&cfg.Answers.Language, a["language"])
		setString(&cfg.Answers.Safesearch, a["safesearch"])
		for _, f := range []struct {
			dst  *int
			key  string
			name string
		}{
			{&cfg.Answers.MaxTokens, "max_tokens", "[answers] max_tokens"},
			{&cfg.Answers.ResearchMaxQueries, "research_max_queries", "[answers] research_max_queries"},
			{&cfg.Answers.ResearchMaxIterations, "research_max_iterations", "[answers] research_max_iterations"},
			{&cfg.Answers.ResearchMaxSeconds, "research_max_seconds", "[answers] research_max_seconds"},
		} {
			if err = setInt(f.dst, a[f.key], f.name); err != nil {
				return err
			}
		}
	}
	return nil
}

// Environment variables. BRAVE_SEARCH_API_KEY is the name Brave's own skills
// use, so an environment already set up for them works here unchanged.
const (
	EnvAPIKey        = "BRAVE_SEARCH_API_KEY"
	EnvAnswersAPIKey = "BRAVE_SEARCH_ANSWERS_API_KEY"
	EnvBaseURL       = "BRAVE_SEARCH_BASE_URL"
	EnvAPIVersion    = "BRAVE_SEARCH_API_VERSION"
	EnvTimeout       = "BRAVE_SEARCH_TIMEOUT_SECONDS"
	EnvCountry       = "BRAVE_SEARCH_COUNTRY" // applies to search and answers alike
	EnvSearchLang    = "BRAVE_SEARCH_SEARCH_LANG"
	EnvLanguage      = "BRAVE_SEARCH_LANGUAGE"
	EnvSafesearch    = "BRAVE_SEARCH_SAFESEARCH" // applies to search and answers alike
	EnvCount         = "BRAVE_SEARCH_COUNT"
)

// EnvVars lists every variable Load reads, so tests can clear them and the
// manual can enumerate them.
func EnvVars() []string {
	return []string{EnvAPIKey, EnvAnswersAPIKey, EnvBaseURL, EnvAPIVersion, EnvTimeout, EnvCountry, EnvSearchLang, EnvLanguage, EnvSafesearch, EnvCount}
}

func applyEnv(cfg *Config) error {
	setString(&cfg.APIKey, os.Getenv(EnvAPIKey))
	setString(&cfg.AnswersAPIKey, os.Getenv(EnvAnswersAPIKey))
	setString(&cfg.BaseURL, os.Getenv(EnvBaseURL))
	setString(&cfg.APIVersion, os.Getenv(EnvAPIVersion))
	if v := os.Getenv(EnvTimeout); v != "" {
		d, err := parseSeconds(v)
		if err != nil {
			return fmt.Errorf("%s: %w", EnvTimeout, err)
		}
		cfg.Timeout = d
	}
	if v := os.Getenv(EnvCountry); v != "" {
		cfg.Search.Country, cfg.Answers.Country = v, v
	}
	setString(&cfg.Search.SearchLang, os.Getenv(EnvSearchLang))
	setString(&cfg.Answers.Language, os.Getenv(EnvLanguage))
	if v := os.Getenv(EnvSafesearch); v != "" {
		cfg.Search.Safesearch, cfg.Answers.Safesearch = v, v
	}
	return setInt(&cfg.Search.Count, os.Getenv(EnvCount), EnvCount)
}

func setString(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

func setInt(dst *int, v, name string) error {
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fmt.Errorf("%s: %q is not an integer", name, v)
	}
	*dst = n
	return nil
}

func parseSeconds(v string) (time.Duration, error) {
	s, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || s <= 0 {
		return 0, fmt.Errorf("%q is not a positive number of seconds", v)
	}
	return time.Duration(s * float64(time.Second)), nil
}

// parseDuration accepts Go durations ("30s", "2m") and bare seconds ("30").
func parseDuration(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if d, err := time.ParseDuration(v); err == nil {
		if d <= 0 {
			return 0, fmt.Errorf("%q is not a positive duration", v)
		}
		return d, nil
	}
	return parseSeconds(v)
}

// SearchPaths lists where a config file is looked for, in order. macOS
// operators expect ~/.config too, so it is searched before Application
// Support rather than instead of it.
func SearchPaths() []string {
	var paths []string
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		paths = append(paths, filepath.Join(x, "brave-search", "config.toml"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return paths
	}
	paths = append(paths, filepath.Join(home, ".config", "brave-search", "config.toml"))
	if runtime.GOOS == "darwin" {
		paths = append(paths, filepath.Join(home, "Library", "Application Support", "brave-search", "config.toml"))
	}
	return paths
}

// FindConfig returns the first existing search path, or "" when none exists.
func FindConfig() string {
	for _, p := range SearchPaths() {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func isSearchPath(p string) bool {
	for _, s := range SearchPaths() {
		if s == p {
			return true
		}
	}
	return false
}

// parseTOML parses the minimal subset this tool needs: [section] headers and
// key = value lines, where value is an optionally quoted string. Comments start
// with '#'. It intentionally does not support arrays, nested tables, or typed
// values. Vendored from the sibling tools rather than imported, matching the
// series.
func parseTOML(r io.Reader) (map[string]map[string]string, error) {
	sections := map[string]map[string]string{}
	current := ""
	sections[current] = map[string]string{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		if strings.HasPrefix(raw, "[") {
			end := strings.IndexByte(raw, ']')
			if end < 0 {
				return nil, fmt.Errorf("line %d: unterminated section header", line)
			}
			current = strings.TrimSpace(raw[1:end])
			if _, ok := sections[current]; !ok {
				sections[current] = map[string]string{}
			}
			continue
		}
		eq := strings.IndexByte(raw, '=')
		if eq < 0 {
			return nil, fmt.Errorf("line %d: expected key = value", line)
		}
		key := strings.TrimSpace(raw[:eq])
		val := parseValue(strings.TrimSpace(raw[eq+1:]))
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", line)
		}
		sections[current][key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return sections, nil
}

// parseValue strips surrounding quotes, or trims a trailing inline comment from
// a bare value.
func parseValue(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		q := v[0]
		if end := strings.IndexByte(v[1:], q); end >= 0 {
			return v[1 : 1+end]
		}
	}
	if hash := strings.IndexByte(v, '#'); hash >= 0 {
		v = strings.TrimSpace(v[:hash])
	}
	return v
}
