//go:build !windows

package runtime

import "testing"

func TestReadDiskStatsReturnsErrorForInvalidPath(t *testing.T) {
	_, err := readDiskStats("/nonexistent/veil-runtime-test-path/invalid")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}
