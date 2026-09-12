package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/nlink-jp/brave-search/internal/brave"
	"github.com/nlink-jp/brave-search/internal/config"
	"github.com/nlink-jp/brave-search/internal/engine"
)

// commonFlags are the flags every search command shares, so `--json`,
// `--config`, `--timeout`, `--country`, `--lang` and `--safesearch` mean the
// same thing everywhere rather than being re-declared (and eventually
// diverging) per command.
type commonFlags struct {
	jsonOut    bool
	timeout    time.Duration
	config     string
	country    string
	lang       string
	safesearch string
}

func (c *commonFlags) register(fs *flag.FlagSet) {
	fs.BoolVar(&c.jsonOut, "json", false, "JSON output")
	fs.BoolVar(&c.jsonOut, "j", false, "JSON output (shorthand)")
	fs.DurationVar(&c.timeout, "timeout", 0, "deadline per exchange (e.g. 10s)")
	fs.StringVar(&c.config, "config", "", "config file path")
	fs.StringVar(&c.config, "c", "", "config file path (shorthand)")
	fs.StringVar(&c.country, "country", "", "two-letter country code or ALL")
	fs.StringVar(&c.lang, "lang", "", "search language (web, context) or reply language (answer, research)")
	fs.StringVar(&c.safesearch, "safesearch", "", "off | moderate | strict")
}

// build resolves configuration and wires the engine.
func (c *commonFlags) build(version string) (*config.Config, *engine.Engine, error) {
	cfg, err := config.Load(c.config, c.timeout)
	if err != nil {
		return nil, nil, err
	}
	return cfg, engine.New(cfg, newClient(cfg, version)), nil
}

// newClient wires the upstream client from a configuration.
func newClient(cfg *config.Config, version string) *brave.Client {
	c := brave.New(cfg.BaseURL, cfg.APIKey, cfg.APIVersion, cfg.Timeout, "brave-search/"+version)
	c.AnswersAPIKey = cfg.AnswersAPIKey
	return c
}

// newFlagSet builds a flag set that reports errors on stderr.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parseInterleaved parses flags that appear anywhere among the positional
// arguments, and returns the positionals.
//
// Go's flag package stops at the first non-flag argument, so a plain Parse
// would read `web "go generics" --count 3` as two positionals and silently
// ignore the count. Writing the query first is the natural way to type this,
// and a flag that is quietly dropped is worse than one that is rejected — so
// parse in rounds, taking one positional at a time. A bare `--` ends flag
// parsing for everything after it, which is how a query word that starts
// with `-` (the exclusion operator, `-inurl:x`) is passed.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		// fs.Parse consumed a leading "--" itself; if the argument it
		// stopped on was preceded by one, everything left is positional.
		if consumedTerminator(fs, rest, args) {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
}

// consumedTerminator reports whether the round that produced rest ended on a
// "--" terminator: fs.Args() then starts right after it, which is visible as
// the argument before rest[0] in the original list being "--".
func consumedTerminator(fs *flag.FlagSet, rest, args []string) bool {
	idx := len(args) - len(rest) - 1
	return idx >= 0 && args[idx] == "--"
}

// parseCommand parses a command's flags and positionals and applies the
// shared conventions: `-h`/`--help` prints the usage on stdout and succeeds,
// any other parse error is a usage error. ok is false when the caller should
// return code.
func parseCommand(fs *flag.FlagSet, args []string, stdout, stderr io.Writer) (positional []string, code int, ok bool) {
	// The flag package prints its own error before calling Usage; silence
	// both so the error is reported once, in the tool's voice.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	positional, err := parseInterleaved(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		usage(stdout)
		return nil, exitOK, false
	}
	if err != nil {
		return nil, fail(stderr, exitError, "%v", err), false
	}
	return positional, 0, true
}

// queryArg joins the positionals into the query. Accepting several words
// unquoted is friendlier than demanding quotes, and the engine enforces the
// length limits either way.
func queryArg(positional []string, stderr io.Writer, what string) (string, bool) {
	q := joinWords(positional)
	if q == "" {
		fail(stderr, exitError, "a %s is required", what)
		return "", false
	}
	return q, true
}

func joinWords(ws []string) string {
	out := ""
	for _, w := range ws {
		if out != "" {
			out += " "
		}
		out += w
	}
	return out
}

// fail prints a one-line error in the tool's voice and returns the code.
func fail(stderr io.Writer, code int, format string, args ...any) int {
	fmt.Fprintf(stderr, "brave-search: "+format+"\n", args...)
	return code
}

// failErr maps an error onto the exit contract: a local or upstream argument
// problem and a missing key are usage errors (2); everything else is an
// upstream failure (1).
func failErr(stderr io.Writer, err error) int {
	code := exitUpstream
	switch brave.Code(err) {
	case brave.CodeInvalidArguments, brave.CodeMissingAPIKey:
		code = exitError
	}
	if d := detailsHint(err); d != "" {
		return fail(stderr, code, "%v\n  %s", err, d)
	}
	return fail(stderr, code, "%v", err)
}

// detailsHint turns the machine-readable details into one human line.
func detailsHint(err error) string {
	var e *brave.Error
	if !asBraveError(err, &e) || e.Details == nil {
		return ""
	}
	if s, ok := e.Details["reset_seconds"]; ok {
		return fmt.Sprintf("rate limit resets in %vs", s)
	}
	if c, ok := e.Details["upstream_code"]; ok {
		return fmt.Sprintf("upstream code: %v", c)
	}
	return ""
}

func asBraveError(err error, target **brave.Error) bool {
	e, ok := err.(*brave.Error)
	if !ok {
		return false
	}
	*target = e
	return true
}

// writeJSON prints a result as indented JSON.
func writeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fail(stderr, exitError, "%v", err)
	}
	return exitOK
}
