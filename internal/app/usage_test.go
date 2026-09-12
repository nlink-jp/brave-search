package app

import (
	"strings"
	"testing"

	"github.com/nlink-jp/brave-search/internal/config"
	"github.com/nlink-jp/brave-search/internal/mcp"
)

// The manual meta-tests in internal/mcp only see get_usage, because the real
// tool set is assembled here with a configuration. These pin the manual to
// the tools an agent actually gets.

func realTools(t *testing.T) []mcp.Tool {
	t.Helper()
	return mcp.New("t", tools(config.Defaults(), "t")...).Tools()
}

func TestUsageDocumentsEveryRealTool(t *testing.T) {
	doc := mcp.UsageDoc()
	for _, tool := range realTools(t) {
		if !strings.Contains(doc, "### `"+tool.Name+"`") {
			t.Errorf("usage.md has no section for the tool %q", tool.Name)
		}
		props, _ := tool.InputSchema["properties"].(map[string]any)
		for arg := range props {
			if !strings.Contains(doc, "`"+arg+"`") {
				t.Errorf("usage.md never mentions %s's argument %q", tool.Name, arg)
			}
		}
		if len(tool.Description) < 40 {
			t.Errorf("%s has a description too short to be useful", tool.Name)
		}
		required, _ := tool.InputSchema["required"].([]string)
		for _, r := range required {
			if _, ok := props[r]; !ok {
				t.Errorf("%s requires %q but does not declare it", tool.Name, r)
			}
		}
	}
}

// Every tool that spends money has to say so where a tools/list-only reader
// sees it.
func TestEveryBilledToolDescriptionMentionsCost(t *testing.T) {
	for _, tool := range realTools(t) {
		if tool.Name == mcp.ToolGetUsage {
			continue
		}
		if !strings.Contains(tool.Description, "billed") && !strings.Contains(tool.Description, "cost") {
			t.Errorf("%s's description does not mention that the call is billed: %q", tool.Name, tool.Description)
		}
	}
}
