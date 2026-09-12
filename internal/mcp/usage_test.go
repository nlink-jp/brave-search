package mcp

import (
	"strings"
	"testing"
)

// These are meta-tests: they pin the embedded manual to the code. The
// mcp-tactics skill documents no parameters by design, so usage.md is what an
// agent reads before its first call — a manual that drifts from the
// implementation is worse than none, because it is trusted.

func TestUsageDocumentsEveryTool(t *testing.T) {
	doc := UsageDoc()
	for _, tool := range New("test").Tools() {
		if !strings.Contains(doc, "`"+tool.Name+"`") {
			t.Errorf("usage.md never mentions the tool %q", tool.Name)
		}
	}
}

func TestUsageDocumentsEveryArgument(t *testing.T) {
	doc := UsageDoc()
	for _, tool := range New("test").Tools() {
		props, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			continue
		}
		for arg := range props {
			if !strings.Contains(doc, "`"+arg+"`") {
				t.Errorf("usage.md never mentions %s's argument %q", tool.Name, arg)
			}
		}
	}
}

func TestUsageDocumentsEveryErrorCode(t *testing.T) {
	doc := UsageDoc()
	for _, c := range append([]string{CodeInvalidArguments}, upstreamCodes()...) {
		if !strings.Contains(doc, "`"+c+"`") {
			t.Errorf("usage.md has no recovery guidance for the error code %q", c)
		}
	}
}

// The facts that decide whether an agent uses this server responsibly have to
// survive in the manual and in the initialize instructions, since a model may
// read either one alone.
func TestCostAndToSCaveatsAreEverywhereAnAgentLooks(t *testing.T) {
	for _, word := range []string{"cost", "cached", "cancelled", "train"} {
		if !strings.Contains(UsageDoc(), word) {
			t.Errorf("usage.md no longer mentions %q", word)
		}
	}
	for _, phrase := range []string{"EVERY CALL COSTS MONEY", "Nothing is cached", "train"} {
		if !strings.Contains(Instructions, phrase) {
			t.Errorf("the server instructions no longer say %q", phrase)
		}
	}
}

func TestToolDescriptionsAreSubstantial(t *testing.T) {
	for _, tool := range New("test").Tools() {
		if len(tool.Description) < 40 {
			t.Errorf("%s has a description too short to be useful: %q", tool.Name, tool.Description)
		}
	}
}

func TestRequiredArgumentsAreDeclared(t *testing.T) {
	for _, tool := range New("test").Tools() {
		required, _ := tool.InputSchema["required"].([]string)
		props, _ := tool.InputSchema["properties"].(map[string]any)
		for _, r := range required {
			if _, ok := props[r]; !ok {
				t.Errorf("%s requires %q but does not declare it", tool.Name, r)
			}
		}
	}
}
