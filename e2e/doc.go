// Package e2e holds live end-to-end tests that query the real Brave Search
// API. They are guarded by the `e2e` build tag so `go test ./...` (offline)
// never runs them; run them deliberately with:
//
//	make e2e          # or: go test -tags e2e -count=1 ./e2e/...
//
// They need a configured API key and skip cleanly without one. They assert on
// live responses and never save them: the Brave ToS permits only transient
// retention of search results, so no live body may become a fixture.
//
// This file has no build tag so the package always compiles (and reports "no
// test files" without the tag).
package e2e
