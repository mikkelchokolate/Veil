package privileged

import (
	"strings"
	"testing"
)

func TestBoundedJournalLinesBoundsIndividualAndTotalOutput(t *testing.T) {
	lines := boundedJournalLines(strings.Repeat("a", 100)+"\n"+strings.Repeat("b", 100), 80, 40)
	joined := strings.Join(lines, "\n")
	if len(joined) > 80 || !strings.Contains(joined, "[TRUNCATED]") {
		t.Fatalf("len=%d output=%q", len(joined), joined)
	}
}
