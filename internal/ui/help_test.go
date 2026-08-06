package ui

import (
	"strings"
	"testing"
)

func TestRenderHelpMenuIncludesEveryCategory(t *testing.T) {
	rendered := renderHelpMenu()
	for _, category := range HelpData {
		if !containsText(rendered, category.Title) {
			t.Errorf("help menu does not include category %q", category.Title)
		}
	}
}

func containsText(text, expected string) bool {
	return len(expected) > 0 && len(text) >= len(expected) &&
		strings.Contains(text, expected)
}
