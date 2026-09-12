package app

import "io"

// The Answers commands land in the Answers unit of Phase 1; auth check in the
// last unit. Until then they refuse loudly rather than pretend.

func runAnswer(_ []string, _ string, _, stderr io.Writer) int {
	return notImplemented(stderr, "answer")
}
func runResearch(_ []string, _ string, _, stderr io.Writer) int {
	return notImplemented(stderr, "research")
}
func runAuth(_ []string, _ string, _, stderr io.Writer) int { return notImplemented(stderr, "auth") }
