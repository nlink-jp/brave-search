package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nlink-jp/brave-search/internal/config"
)

// authUpstream answers the probe per endpoint with a chosen status.
func authUpstream(t *testing.T, web, answers int) (search, answersKey *string) {
	t.Helper()
	isolate(t)
	var seenSearch, seenAnswers string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := web
		if r.URL.Path == "/chat/completions" {
			status = answers
			seenAnswers = r.Header.Get("X-Subscription-Token")
		} else {
			seenSearch = r.Header.Get("X-Subscription-Token")
		}
		// 4220 stands for Brave's real shape: 422 with SUBSCRIPTION_TOKEN_INVALID.
		if status == 4220 {
			w.WriteHeader(422)
			_, _ = w.Write([]byte(`{"error":{"code":"SUBSCRIPTION_TOKEN_INVALID","detail":"The provided subscription token is invalid."}}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"code":"C","detail":"d"}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(config.EnvBaseURL, srv.URL)
	return &seenSearch, &seenAnswers
}

func TestAuthCheckBothPlansValid(t *testing.T) {
	seenSearch, seenAnswers := authUpstream(t, 422, 422)
	t.Setenv(config.EnvAPIKey, "sk")
	t.Setenv(config.EnvAnswersAPIKey, "ak")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"auth", "check"}, "t", nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s%s", code, stdout.String(), stderr.String())
	}
	if *seenSearch != "sk" || *seenAnswers != "ak" {
		t.Errorf("keys sent: search=%q answers=%q", *seenSearch, *seenAnswers)
	}
	out := stdout.String()
	for _, want := range []string{"config file: none found (searched:", "Search:  valid", "Answers: valid", "spent nothing"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestAuthCheckDistinguishesTheFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		web, answers int
		searchKey    string
		wantCode     int
		wantWords    []string
	}{
		"rejected search key":    {4220, 422, "sk", exitError, []string{"Search:  REJECTED", "Answers: valid"}},
		"answers not subscribed": {422, 403, "sk", exitError, []string{"Search:  valid", "Answers: NOT_SUBSCRIBED", "one key per plan"}},
		"search key absent":      {422, 422, "", exitError, []string{"Search:  ABSENT", "BRAVE_SEARCH_API_KEY", "Answers: valid"}},
		"upstream down":          {503, 422, "sk", exitUpstream, []string{"Search:  UNREACHABLE", "Answers: valid"}},
		"rate limited is valid":  {429, 429, "sk", exitOK, []string{"Search:  valid", "rate-limited"}},
	} {
		authUpstream(t, tc.web, tc.answers)
		t.Setenv(config.EnvAPIKey, tc.searchKey)
		t.Setenv(config.EnvAnswersAPIKey, "ak")
		var stdout, stderr bytes.Buffer
		if code := run([]string{"auth", "check"}, "t", nil, &stdout, &stderr); code != tc.wantCode {
			t.Errorf("%s: exit %d, want %d\n%s%s", name, code, tc.wantCode, stdout.String(), stderr.String())
		}
		for _, want := range tc.wantWords {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("%s: output lacks %q:\n%s", name, want, stdout.String())
			}
		}
	}
}

func TestAuthCheckJSONAndConfigPath(t *testing.T) {
	dir := t.TempDir()
	authUpstream(t, 422, 401)
	path := filepath.Join(dir, "c.toml")
	if err := writeFile(path, "[api]\napi_key = \"sk\"\nanswers_api_key = \"bad\"\n"); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"auth", "check", "--json", "--config", path}, "t", nil, &stdout, &stderr); code != exitError {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"config_file": "` + path + `"`, `"ok": false`, `"status": "valid"`, `"status": "rejected"`, `"http_status": 401`} {
		if !strings.Contains(out, want) {
			t.Errorf("json lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "sk") || strings.Contains(out, "bad") {
		t.Error("a key value leaked into the output")
	}
}

func TestAuthRequiresCheck(t *testing.T) {
	isolate(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"auth"}, "t", nil, &stdout, &stderr); code != exitError {
		t.Errorf("exit %d", code)
	}
}
