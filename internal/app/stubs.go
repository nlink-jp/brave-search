package app

import "io"

// The four search commands land in the Search and Answers units of Phase 1.
// Until then they refuse loudly rather than pretend.

func runWeb(_ []string, _ string, _, stderr io.Writer) int { return notImplemented(stderr, "web") }
func runContext(_ []string, _ string, _, stderr io.Writer) int {
	return notImplemented(stderr, "context")
}
func runAnswer(_ []string, _ string, _, stderr io.Writer) int {
	return notImplemented(stderr, "answer")
}
func runResearch(_ []string, _ string, _, stderr io.Writer) int {
	return notImplemented(stderr, "research")
}
func runAuth(_ []string, _ string, _, stderr io.Writer) int { return notImplemented(stderr, "auth") }
