//go:build unix

package acmeip

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCanceledACMECommandStopsChild(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "late")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := defaultSystem{}.CombinedOutputContext(ctx, "sh", "-c", `sleep 1; printf late > "$1"`, "audit", sentinel)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Fatal("child command wrote a file AFTER runWithContext returned")
	}
}

func TestRunWithContextWaitsForKilledProcessBeforeReturn(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "late")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := runWithContext(ctx, defaultSystem{}, "sh", "-c", `sleep 1; printf late > "$1"`, "audit", sentinel)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("runWithContext took %s, expected the process group to be killed promptly", elapsed)
	}
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Fatal("killed issuer still wrote files after return")
	}
}
