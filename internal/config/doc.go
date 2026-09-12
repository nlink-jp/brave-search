// Package config resolves brave-search's runtime settings from a sectioned
// TOML file, BRAVE_SEARCH_* environment variables and built-in defaults, in
// that order of precedence (flags, applied by the caller, win over all three).
//
// The keys mirror the parameter matrix in the RFP: [api], [search],
// [context] and [answers]. There is deliberately no [cache] section — the
// Brave ToS permits only transient retention of search results, so nothing is
// written to disk.
package config
