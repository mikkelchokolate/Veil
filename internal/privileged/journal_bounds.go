package privileged

import "strings"

func boundedJournalLines(output string, maxTotalBytes, maxLineBytes int) []string {
	if maxTotalBytes <= 0 || maxLineBytes <= 0 {
		return nil
	}
	const marker = "...[TRUNCATED]"
	lines := make([]string, 0)
	used := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		if len(line) > maxLineBytes {
			line = strings.ToValidUTF8(line[:maxLineBytes-len(marker)], "") + marker
		}
		cost := len(line)
		if len(lines) > 0 {
			cost++
		}
		if used+cost > maxTotalBytes {
			if used+len(marker)+1 <= maxTotalBytes {
				lines = append(lines, marker)
			}
			break
		}
		lines = append(lines, line)
		used += cost
	}
	return lines
}
