package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/nlink-jp/brave-search/internal/engine"
)

// runWeb implements `web <query>`.
func runWeb(args []string, version string, stdout, stderr io.Writer) int {
	var f commonFlags
	var r engine.WebRequest
	fs := newFlagSet("web", stderr)
	f.register(fs)
	fs.IntVar(&r.Count, "count", 0, "results, 1-20")
	fs.IntVar(&r.Offset, "offset", 0, "page, 0-9")
	fs.StringVar(&r.Freshness, "freshness", "", "pd | pw | pm | py | YYYY-MM-DDtoYYYY-MM-DD")
	fs.BoolVar(&r.ExtraSnippets, "extra-snippets", false, "up to five extra excerpts per result")
	fs.BoolVar(&r.Full, "full", false, "pass every upstream field through")

	positional, err := parseInterleaved(fs, args)
	if err != nil {
		return exitError
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
	res, err := eng.Web(context.Background(), r)
	if err != nil {
		return failErr(stderr, err)
	}
	if f.jsonOut {
		return writeJSON(stdout, stderr, res)
	}
	renderWeb(stdout, res)
	return exitOK
}

func renderWeb(w io.Writer, res *engine.WebResult) {
	fmt.Fprintf(w, "# %s", res.Query)
	if res.Altered != "" {
		fmt.Fprintf(w, "   (searched as: %s)", res.Altered)
	}
	fmt.Fprintln(w)
	if len(res.Results) == 0 {
		fmt.Fprintln(w, "no results")
	}
	for _, h := range res.Results {
		fmt.Fprintf(w, "\n%d. %s\n   %s\n", h.Rank, h.Title, h.URL)
		if h.Description != "" {
			fmt.Fprintf(w, "   %s\n", h.Description)
		}
		var facts []string
		if h.Age != "" {
			facts = append(facts, h.Age)
		}
		if h.Language != "" {
			facts = append(facts, "lang: "+h.Language)
		}
		if len(facts) > 0 {
			fmt.Fprintf(w, "   %s\n", strings.Join(facts, " · "))
		}
		for _, s := range h.ExtraSnippets {
			fmt.Fprintf(w, "   + %s\n", s)
		}
	}
	if res.MoreResultsAvailable {
		fmt.Fprintf(w, "\nmore results available: --offset %d\n", res.Offset+1)
	}
	fmt.Fprintf(w, "\n%s\n", res.Meta.String())
}
