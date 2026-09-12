package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolate points HOME and XDG_CONFIG_HOME at a tempdir and clears every
// BRAVE_SEARCH_* variable, so no test can read the developer's real config or
// key. Both HOME and XDG are replaced: replacing one alone picks up the other.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	for _, v := range EnvVars() {
		t.Setenv(v, "")
	}
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultsAreValidAndMatchTheRFP(t *testing.T) {
	isolate(t)
	cfg, err := Load("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != "" {
		t.Errorf("Path = %q, want none read", cfg.Path)
	}
	if cfg.HasKey() {
		t.Error("a key appeared from nowhere")
	}
	if cfg.BaseURL != DefaultBaseURL || cfg.Timeout != 30*time.Second || cfg.APIVersion != "" {
		t.Errorf("api defaults: %+v", cfg.Redacted())
	}
	if cfg.Search != (Search{Country: "US", SearchLang: "en", Safesearch: "moderate", Count: 10}) {
		t.Errorf("search defaults: %+v", cfg.Search)
	}
	if cfg.Context != (Context{MaxTokens: 8192, MaxURLs: 20}) {
		t.Errorf("context defaults: %+v", cfg.Context)
	}
	want := Answers{Country: "US", Language: "en", Safesearch: "moderate", ResearchMaxQueries: 10, ResearchMaxIterations: 2, ResearchMaxSeconds: 120}
	if cfg.Answers != want {
		t.Errorf("answers defaults: %+v", cfg.Answers)
	}
}

func TestFileThenEnvThenOverride(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "xdg", "brave-search", "config.toml")
	write(t, path, `
[api]
api_key = "file-key"
api_version = "2026-09-12"
timeout = "45s"

[search]
country = "JP"   # inline comment
search_lang = "ja"
count = 5

[context]
max_tokens = 4096
max_urls = 7

[answers]
country = "JP"
language = "ja"
max_tokens = 512
research_max_queries = 3
research_max_iterations = 1
research_max_seconds = 60
`)
	t.Setenv(EnvSearchLang, "de")
	t.Setenv(EnvCount, "3")

	cfg, err := Load("", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != path {
		t.Errorf("Path = %q, want %q", cfg.Path, path)
	}
	if cfg.APIKey != "file-key" || cfg.APIVersion != "2026-09-12" {
		t.Errorf("api: %+v", cfg.Redacted())
	}
	if cfg.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want the override", cfg.Timeout)
	}
	if cfg.Search.Country != "JP" || cfg.Search.SearchLang != "de" || cfg.Search.Count != 3 {
		t.Errorf("search: %+v", cfg.Search)
	}
	if cfg.Context != (Context{MaxTokens: 4096, MaxURLs: 7}) {
		t.Errorf("context: %+v", cfg.Context)
	}
	if cfg.Answers.Language != "ja" || cfg.Answers.MaxTokens != 512 || cfg.Answers.ResearchMaxSeconds != 60 {
		t.Errorf("answers: %+v", cfg.Answers)
	}
}

func TestEnvKeyOverridesFileKey(t *testing.T) {
	dir := isolate(t)
	write(t, filepath.Join(dir, ".config", "brave-search", "config.toml"), "[api]\napi_key = \"file\"\n")
	t.Setenv(EnvAPIKey, "env")
	t.Setenv(EnvAnswersAPIKey, "env-answers")
	cfg, err := Load("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "env" || cfg.AnswersAPIKey != "env-answers" {
		t.Errorf("keys = %q / %q", cfg.APIKey, cfg.AnswersAPIKey)
	}
	if r := cfg.Redacted(); r.APIKey != "[set]" || r.AnswersAPIKey != "[set]" {
		t.Error("Redacted leaked a key")
	}
}

// ~/.config is searched even on macOS, and XDG wins over it.
func TestSearchPathOrder(t *testing.T) {
	dir := isolate(t)
	dot := filepath.Join(dir, ".config", "brave-search", "config.toml")
	xdg := filepath.Join(dir, "xdg", "brave-search", "config.toml")
	write(t, dot, "[search]\ncount = 2\n")
	cfg, err := Load("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != dot || cfg.Search.Count != 2 {
		t.Errorf("~/.config not read: path=%q count=%d", cfg.Path, cfg.Search.Count)
	}
	write(t, xdg, "[search]\ncount = 4\n")
	cfg, err = Load("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != xdg || cfg.Search.Count != 4 {
		t.Errorf("XDG did not win: path=%q count=%d", cfg.Path, cfg.Search.Count)
	}
}

// The shared environment variables fan out to both [search] and [answers],
// and every other variable reaches its field.
func TestEveryEnvironmentVariableIsRead(t *testing.T) {
	isolate(t)
	t.Setenv(EnvCountry, "jp")
	t.Setenv(EnvSafesearch, "strict")
	t.Setenv(EnvLanguage, "ja")
	t.Setenv(EnvAPIVersion, "2025-01-01")
	t.Setenv(EnvTimeout, "7.5")
	t.Setenv(EnvBaseURL, "http://localhost:1")
	cfg, err := Load("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search.Country != "jp" || cfg.Answers.Country != "jp" || cfg.Search.Safesearch != "strict" || cfg.Answers.Safesearch != "strict" {
		t.Errorf("fan-out: %+v / %+v", cfg.Search, cfg.Answers)
	}
	if cfg.Answers.Language != "ja" || cfg.APIVersion != "2025-01-01" || cfg.Timeout != 7500*time.Millisecond || cfg.BaseURL != "http://localhost:1" {
		t.Errorf("%+v", cfg.Redacted())
	}
	t.Setenv(EnvTimeout, "soon")
	if _, err := Load("", 0); err == nil {
		t.Error("a non-numeric timeout was accepted")
	}
}

func TestExplicitMissingConfigIsAnError(t *testing.T) {
	dir := isolate(t)
	if _, err := Load(filepath.Join(dir, "nope.toml"), 0); err == nil {
		t.Error("a missing --config path was accepted silently")
	}
}

func TestTyposAreRejected(t *testing.T) {
	dir := isolate(t)
	for _, body := range []string{
		"[api]\napikey = \"x\"\n",
		"[serach]\ncount = 3\n",
		"count = 3\n",
		"[answers]\nresearch_max_seconds = many\n",
		"[api]\ntimeout = \"-1s\"\n",
	} {
		path := filepath.Join(dir, "c.toml")
		write(t, path, body)
		if _, err := Load(path, 0); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}

func TestRangesAreValidatedLocally(t *testing.T) {
	dir := isolate(t)
	for _, tc := range []struct{ body, want string }{
		{"[search]\ncount = 21\n", "[search] count"},
		{"[search]\nsafesearch = \"maybe\"\n", "safesearch"},
		{"[context]\nmax_tokens = 100\n", "max_tokens"},
		{"[context]\nmax_urls = 51\n", "max_urls"},
		{"[answers]\nresearch_max_queries = 0\n", "research_max_queries"},
		{"[answers]\nresearch_max_iterations = 6\n", "research_max_iterations"},
		{"[answers]\nresearch_max_seconds = 301\n", "research_max_seconds"},
		{"[answers]\nmax_tokens = -1\n", "max_tokens"},
	} {
		path := filepath.Join(dir, "c.toml")
		write(t, path, tc.body)
		_, err := Load(path, 0)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want mention of %s", tc.body, err, tc.want)
		}
	}
}

func TestResearchTimeoutDerivesFromBudget(t *testing.T) {
	cfg := Defaults()
	if got := cfg.ResearchTimeout(0); got != 150*time.Second {
		t.Errorf("default research timeout = %v, want 150s", got)
	}
	if got := cfg.ResearchTimeout(10); got != 40*time.Second {
		t.Errorf("research timeout for 10s = %v, want 40s", got)
	}
}

func TestParseValue(t *testing.T) {
	for in, want := range map[string]string{
		`"a # b"`: "a # b",
		`'x'`:     "x",
		`3 # c`:   "3",
		`plain`:   "plain",
	} {
		if got := parseValue(in); got != want {
			t.Errorf("parseValue(%q) = %q, want %q", in, got, want)
		}
	}
}
