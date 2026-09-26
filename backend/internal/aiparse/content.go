// Package aiparse normalizes model output before feature-owned validation.
// It is provider-neutral and has no network or configuration dependencies.
package aiparse

import "strings"

func NormalizeContent(value string) string {
	withoutThinking := stripThinkingBlocks(value)
	return strings.TrimSpace(stripMarkdownFence(withoutThinking))
}

func ExtractJSONCandidates(content string) []string {
	normalized := NormalizeContent(content)
	var candidates []string
	if normalized != "" {
		candidates = append(candidates, normalized)
	}
	for _, object := range extractJSONObjects(normalized) {
		if !contains(candidates, object) {
			candidates = append(candidates, object)
		}
	}
	return candidates
}

func stripThinkingBlocks(value string) string {
	out := value
	for _, tags := range [][2]string{{"<think>", "</think>"}, {"<thinking>", "</thinking>"}} {
		for {
			lower := strings.ToLower(out)
			start := strings.Index(lower, tags[0])
			end := strings.Index(lower, tags[1])
			if start < 0 || end < start {
				break
			}
			out = out[:start] + " " + out[end+len(tags[1]):]
		}
	}
	return out
}

func stripMarkdownFence(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	trimmed = strings.TrimPrefix(trimmed, "```")
	if newline := strings.Index(trimmed, "\n"); newline >= 0 {
		trimmed = trimmed[newline+1:]
	}
	trimmed = strings.TrimSpace(trimmed)
	trimmed = strings.TrimSuffix(trimmed, "```")
	return strings.TrimSpace(trimmed)
}

func extractJSONObjects(value string) []string {
	var objects []string
	depth := 0
	start := -1
	inString := false
	escaped := false
	for i, r := range value {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		if r == '"' {
			inString = true
			continue
		}
		if r == '{' {
			if depth == 0 {
				start = i
			}
			depth++
			continue
		}
		if r == '}' && depth > 0 {
			depth--
			if depth == 0 && start >= 0 {
				objects = append(objects, value[start:i+1])
				start = -1
			}
		}
	}
	return objects
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
