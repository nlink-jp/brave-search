package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/nlink-jp/brave-search/internal/brave"
	"github.com/nlink-jp/brave-search/internal/config"
)

// The answers `auth check` can give per plan. They are more than a boolean
// because "we could not ask" is not "the key is bad", and "the key is good
// but the plan is not" is a third thing again — collapsing them is how an
// outage gets reported as a rejected credential.
const (
	authValid         = "valid"          // Brave accepted the key
	authRejected      = "rejected"       // 401: Brave refused the key
	authNotSubscribed = "not_subscribed" // 403: key accepted, plan not active for this endpoint
	authUnreachable   = "unreachable"    // Brave could not be asked, or answered something unexpected
	authAbsent        = "absent"         // no key configured; nothing was sent
)

// planStatus is one plan's verdict.
type planStatus struct {
	Plan      string   `json:"plan"`
	Endpoints []string `json:"endpoints"`
	Setting   string   `json:"setting"`
	Status    string   `json:"status"`
	Message   string   `json:"message"`
	Status_   int      `json:"http_status,omitempty"`
}

// authStatus is what `auth check` answers, in JSON form.
type authStatus struct {
	ConfigFile  string       `json:"config_file,omitempty"`
	SearchPaths []string     `json:"config_search_paths"`
	Plans       []planStatus `json:"plans"`
	OK          bool         `json:"ok"`
	Note        string       `json:"note"`
}

// runAuth implements `auth check`.
//
// Brave has no "who am I" endpoint and a real search is billed, so each plan
// is probed with a request that is certain to fail validation: only
// successful responses count against the quota, and the refusal's status
// tells the key's standing. Each plan has its own key, so each is probed on
// its own.
func runAuth(args []string, version string, stdout, stderr io.Writer) int {
	var f commonFlags
	fs := newFlagSet("auth", stderr)
	f.register(fs)
	positional, code, ok := parseCommand(fs, args, stdout)
	if !ok {
		return code
	}
	if len(positional) != 1 || positional[0] != "check" {
		return fail(stderr, exitError, "auth takes one subcommand: check")
	}
	cfg, err := config.Load(f.config, f.timeout)
	if err != nil {
		return fail(stderr, exitError, "%v", err)
	}
	client := newClient(cfg, version)

	st := authStatus{
		ConfigFile:  cfg.Path,
		SearchPaths: config.SearchPaths(),
		Note: "Each plan was probed with a deliberately invalid request. Brave documents that only " +
			"successful responses are billed, so this should have spent nothing. valid means the key " +
			"was accepted and the request refused as intended.",
	}
	st.Plans = append(st.Plans,
		probePlan(client, "Search", []string{"web", "context"}, "[api] api_key / BRAVE_SEARCH_API_KEY", brave.EndpointWebSearch),
		probePlan(client, "Answers", []string{"answer", "research"}, "[api] answers_api_key / BRAVE_SEARCH_ANSWERS_API_KEY", brave.EndpointAnswers),
	)
	code = exitOK
	st.OK = true
	for _, p := range st.Plans {
		switch p.Status {
		case authValid:
		case authUnreachable:
			st.OK = false
			if code == exitOK {
				code = exitUpstream
			}
		default:
			st.OK = false
			code = exitError
		}
	}

	if f.jsonOut {
		if rc := writeJSON(stdout, stderr, st); rc != exitOK {
			return rc
		}
		return code
	}
	renderAuth(stdout, st)
	return code
}

func probePlan(client *brave.Client, plan string, endpoints []string, setting, endpoint string) planStatus {
	p := planStatus{Plan: plan, Endpoints: endpoints, Setting: setting}
	meta, err := client.Probe(context.Background(), endpoint)
	if meta != nil {
		p.Status_ = meta.Status
	}
	switch brave.Code(err) {
	case "":
		p.Status, p.Message = authValid, "the key works (Brave answered the probe with 2xx)."
	case brave.CodeInvalidArguments:
		p.Status, p.Message = authValid, "the key works."
	case brave.CodeRateLimited:
		p.Status, p.Message = authValid, "the key works, but the plan is currently rate-limited."
	case brave.CodeMissingAPIKey:
		p.Status, p.Message = authAbsent, "no key is configured; set "+setting+"."
	case brave.CodeUnauthorized:
		p.Status, p.Message = authRejected, "Brave rejected the key. Check "+setting+" for a typo or a stale value."
	case brave.CodePlanNotSubscribed:
		p.Status, p.Message = authNotSubscribed, "the key was accepted but this plan is not active for it. Brave issues one key per plan — is this the "+plan+" plan's key?"
	default:
		p.Status, p.Message = authUnreachable, fmt.Sprintf("could not determine the key's standing: %v", err)
	}
	return p
}

func renderAuth(w io.Writer, st authStatus) {
	if st.ConfigFile != "" {
		fmt.Fprintf(w, "config file: %s\n", st.ConfigFile)
	} else {
		fmt.Fprintf(w, "config file: none found (searched: %s)\n", strings.Join(st.SearchPaths, ", "))
	}
	for _, p := range st.Plans {
		label := strings.ToUpper(p.Status)
		if p.Status == authValid {
			label = "valid"
		}
		fmt.Fprintf(w, "%-8s %-15s %s\n", p.Plan+":", label, "("+strings.Join(p.Endpoints, ", ")+")")
		fmt.Fprintf(w, "         %s\n", p.Message)
	}
	fmt.Fprintf(w, "\n%s\n", st.Note)
}
