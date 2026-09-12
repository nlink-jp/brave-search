package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/nlink-jp/brave-search/internal/brave"
	"github.com/nlink-jp/brave-search/internal/engine"
)

// runAnswer implements `answer <question>`.
func runAnswer(args []string, version string, stdout, stderr io.Writer) int {
	var f commonFlags
	var r engine.AnswerRequest
	fs := newFlagSet("answer", stderr)
	f.register(fs)
	fs.IntVar(&r.MaxTokens, "max-tokens", 0, "reply token cap (default: the API's)")

	positional, code, ok := parseCommand(fs, args, stdout)
	if !ok {
		return code
	}
	q, ok := queryArg(positional, stderr, "question")
	if !ok {
		return exitError
	}
	r.Question, r.Country, r.Language, r.Safesearch = q, f.country, f.lang, f.safesearch

	_, eng, err := f.build(version)
	if err != nil {
		return fail(stderr, exitError, "%v", err)
	}
	res, err := eng.Answer(context.Background(), r)
	if err != nil {
		return failErr(stderr, err)
	}
	if f.jsonOut {
		return writeJSON(stdout, stderr, res)
	}
	renderAnswer(stdout, res)
	return exitOK
}

// runResearch implements `research <question>`.
func runResearch(args []string, version string, stdout, stderr io.Writer) int {
	var f commonFlags
	var r engine.ResearchRequest
	fs := newFlagSet("research", stderr)
	f.register(fs)
	fs.IntVar(&r.MaxQueries, "max-queries", 0, "searches per iteration, 1-50")
	fs.IntVar(&r.MaxIterations, "max-iterations", 0, "iterations, 1-5")
	fs.IntVar(&r.MaxSeconds, "max-seconds", 0, "time budget in seconds, 1-300")

	positional, code, ok := parseCommand(fs, args, stdout)
	if !ok {
		return code
	}
	q, ok := queryArg(positional, stderr, "question")
	if !ok {
		return exitError
	}
	r.Question, r.Country, r.Language, r.Safesearch = q, f.country, f.lang, f.safesearch
	r.Timeout = f.timeout // an explicit --timeout beats the derived deadline

	_, eng, err := f.build(version)
	if err != nil {
		return fail(stderr, exitError, "%v", err)
	}
	// Progress goes to stderr as it arrives: a silent wait of up to five
	// minutes is indistinguishable from a hang, and stdout stays clean for
	// the result.
	progress := func(p brave.Progress) {
		fmt.Fprintf(stderr, "research: %s\n", p.String())
	}
	res, err := eng.Research(context.Background(), r, progress)
	if err != nil {
		return failErr(stderr, err)
	}
	if f.jsonOut {
		return writeJSON(stdout, stderr, res)
	}
	renderAnswer(stdout, res)
	return exitOK
}

func renderAnswer(w io.Writer, res *engine.AnswerResult) {
	fmt.Fprintf(w, "# %s\n\n", res.Question)
	if res.Answer == "" {
		fmt.Fprintln(w, "(no answer text)")
	} else {
		fmt.Fprintln(w, res.Answer)
	}
	if len(res.Citations) > 0 {
		fmt.Fprintln(w, "\nSources:")
		for _, c := range res.Citations {
			line := fmt.Sprintf("  [%d] %s", c.Number, c.URL)
			if s := strings.TrimSpace(c.Snippet); s != "" {
				line += " — " + s
			}
			fmt.Fprintln(w, line)
		}
	}
	if res.Blindspots != "" {
		fmt.Fprintf(w, "\nBlind spots (declared by Brave):\n%s\n", res.Blindspots)
	}
	if res.Note != "" {
		fmt.Fprintf(w, "\nnote: %s\n", res.Note)
	}
	fmt.Fprintf(w, "\n%s\n", res.Meta.String())
}
