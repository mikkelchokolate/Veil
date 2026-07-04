package install

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type errReader struct {
	err error
}

func (e errReader) Read([]byte) (int, error) {
	return 0, e.err
}

func TestConfirmPlanBranches(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		interactive bool
		wantErr     string
	}{
		{
			name:        "cancels on lowercase n",
			in:          "n\n",
			interactive: true,
			wantErr:     "install cancelled",
		},
		{
			name:        "cancels on mixed case no",
			in:          "No\n",
			interactive: true,
			wantErr:     "install cancelled",
		},
		{
			name:        "cancels on empty answer",
			in:          "\n",
			interactive: true,
			wantErr:     "install cancelled",
		},
		{
			name:        "returns read error",
			in:          "",
			interactive: true,
			wantErr:     "read confirmation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := ConfirmPlan(strings.NewReader(tt.in), &out, tt.interactive)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestConfirmPlanReadErrorCustom(t *testing.T) {
	var out bytes.Buffer
	err := ConfirmPlan(errReader{err: errors.New("injected read failure")}, &out, true)
	if err == nil || !strings.Contains(err.Error(), "read confirmation") {
		t.Fatalf("expected read confirmation error, got %v", err)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) && !strings.Contains(err.Error(), "injected") {
		// The wrapped error should preserve the original cause.
		t.Fatalf("expected wrapped cause in error: %v", err)
	}
}
