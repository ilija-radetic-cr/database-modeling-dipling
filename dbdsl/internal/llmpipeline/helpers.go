package llmpipeline

import (
	"strings"
)

func resolvedPromptVersion(value string) string {
	if strings.TrimSpace(value) == "" {
		return promptTemplateVersion
	}
	return strings.TrimSpace(value)
}

func newStageQA(warnings []string) StageQA {
	return StageQA{Errors: []string{}, Warnings: append([]string(nil), warnings...), Coverage: map[string]int{}}
}

func containsString(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}
