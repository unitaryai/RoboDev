package diagnosis

// sanitiseForPrompt truncates s to maxLen and strips ASCII control characters
// (< 0x20 except \n and \t) to prevent escape from prompt delimiters.
func sanitiseForPrompt(s string, maxLen int) string {
	// Strip control characters: keep runes >= 0x20 or newline/tab.
	runes := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 0x20 || r == '\n' || r == '\t' {
			runes = append(runes, r)
		}
	}

	// Truncate to maxLen characters (not bytes) with "..." suffix if needed.
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return string(runes)
}
