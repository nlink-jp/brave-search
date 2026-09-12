package app

import (
	"flag"
	"fmt"
	"io"
)

// newFlagSet builds a flag set whose usage is the global usage text on stderr.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { usage(stderr) }
	return fs
}

// parseInterleaved parses flags that appear anywhere among the positional
// arguments, and returns the positionals.
//
// Go's flag package stops at the first non-flag argument, so a plain Parse
// would read `web "go generics" --count 3` as two positionals and silently
// ignore the count. Writing the query first is the natural way to type this,
// and a flag that is quietly dropped is worse than one that is rejected — so
// parse in rounds, taking one positional at a time.
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
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
}

// fail prints a one-line error in the tool's voice and returns the code.
func fail(stderr io.Writer, code int, format string, args ...any) int {
	fmt.Fprintf(stderr, "brave-search: "+format+"\n", args...)
	return code
}

// notImplemented is the scaffold's honest answer for a command that exists in
// the interface but not yet in the code: a usage error, never a silent zero.
func notImplemented(stderr io.Writer, cmd string) int {
	return fail(stderr, exitError, "%s is not implemented yet", cmd)
}
