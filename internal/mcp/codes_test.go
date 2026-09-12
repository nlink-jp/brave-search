package mcp

// upstreamCodes lists the error codes the brave package can surface. It lives
// in a test file of this package (rather than importing brave) so the manual
// meta-test stays decoupled from the client; the brave package's own test pins
// the same list to its constants.
func upstreamCodes() []string {
	return []string{
		"missing_api_key",
		"unauthorized",
		"plan_not_subscribed",
		"rate_limited",
		"upstream_error",
		"timeout",
		"network_error",
		"decode_error",
	}
}
