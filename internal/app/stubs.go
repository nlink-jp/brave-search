package app

import "io"

// auth check lands in the last unit of Phase 1. Until then it refuses loudly
// rather than pretend.

func runAuth(_ []string, _ string, _, stderr io.Writer) int { return notImplemented(stderr, "auth") }
