package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/nlink-jp/brave-search/internal/engine"
)

// runContext implements `context <query>`.
func runContext(args []string, version string, stdout, stderr io.Writer) int {
	var f commonFlags
	var r engine.ContextRequest
	fs := newFlagSet("context", stderr)
	f.register(fs)
	fs.IntVar(&r.Count, "count", 0, "results to analyse, 1-50")
	fs.StringVar(&r.Freshness, "freshness", "", "pd | pw | pm | py | YYYY-MM-DDtoYYYY-MM-DD")
	fs.IntVar(&r.MaxTokens, "max-tokens", 0, "context budget, 1024-32768")
	fs.IntVar(&r.MaxURLs, "max-urls", 0, "URLs to draw from, 1-50")

	positional, code, ok := parseCommand(fs, args, stdout, stderr)
	if !ok {
		return code
	}
	q, ok := queryArg(positional, stderr, "query")
	if !ok {
		return exitError
	}
	r.Query, r.Country, r.SearchLang, r.Safesearch = q, f.country, f.lang, f.safesearch

	_, eng, err := f.build(version)
	if err != nil {
		return fail(stderr, exitError, "%v", err)
	}
	res, err := eng.Context(context.Background(), r)
	if err != nil {
		return failErr(stderr, err)
	}
	if f.jsonOut {
		return writeJSON(stdout, stderr, res)
	}
	renderContext(stdout, res)
	return exitOK
}

func renderContext(w io.Writer, res *engine.ContextResult) {
	fmt.Fprintf(w, "# %s   (%d URLs, %d snippets)\n", res.Query, res.URLs, res.Snippets)
	if len(res.Chunks) == 0 {
		fmt.Fprintln(w, "no context")
	}
	for i, c := range res.Chunks {
		fmt.Fprintf(w, "\n## [%d] %s\n%s", i+1, c.Title, c.URL)
		var facts []string
		if c.Hostname != "" {
			facts = append(facts, c.Hostname)
		}
		if c.Age != "" {
			facts = append(facts, c.Age)
		}
		if len(facts) > 0 {
			fmt.Fprintf(w, "   (%s)", strings.Join(facts, " · "))
		}
		fmt.Fprintln(w)
		for _, s := range c.Snippets {
			fmt.Fprintf(w, "\n%s\n", s)
		}
	}
	fmt.Fprintf(w, "\n%s\n", res.Meta.String())
}
