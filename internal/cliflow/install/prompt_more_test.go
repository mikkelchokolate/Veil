package install

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestPromptMissingOptionsBranches(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		opts        PromptOptions
		want        func(t *testing.T, opts PromptOptions)
		wantErr     bool
		errContains string
		wantOutput  string
	}{
		{
			name: "invalid access mode then local",
			in:   "bad\nlocal\n",
			opts: PromptOptions{PanelPortSet: true},
			want: func(t *testing.T, opts PromptOptions) {
				if opts.PanelAccess != "local" {
					t.Fatalf("expected PanelAccess local, got %q", opts.PanelAccess)
				}
			},
			wantOutput: "Panel access mode must be local, direct, or caddy.",
		},
		{
			name: "empty answers default to local and random",
			in:   "\n\n",
			opts: PromptOptions{},
			want: func(t *testing.T, opts PromptOptions) {
				if opts.PanelAccess != "local" {
					t.Fatalf("expected PanelAccess local, got %q", opts.PanelAccess)
				}
				if opts.PanelPort != 0 {
					t.Fatalf("expected PanelPort 0, got %d", opts.PanelPort)
				}
			},
		},
		{
			name: "invalid port mode then random",
			in:   "bad\nrandom\n",
			opts: PromptOptions{PanelAccessSet: true, PanelAccess: "local"},
			want: func(t *testing.T, opts PromptOptions) {
				if opts.PanelPort != 0 {
					t.Fatalf("expected PanelPort 0, got %d", opts.PanelPort)
				}
			},
			wantOutput: "Panel port mode must be random or custom.",
		},
		{
			name: "custom port with non-numeric and out-of-range then valid",
			in:   "custom\nabc\n0\n70000\n3096\n",
			opts: PromptOptions{PanelAccessSet: true, PanelAccess: "direct"},
			want: func(t *testing.T, opts PromptOptions) {
				if opts.PanelPort != 3096 {
					t.Fatalf("expected PanelPort 3096, got %d", opts.PanelPort)
				}
			},
			wantOutput: "Port must be a number",
		},
		{
			name: "caddy with empty invalid and valid domain plus email",
			in:   "\nbad domain\nexample.com\nadmin@example.com\n",
			opts: PromptOptions{PanelAccessSet: true, PanelAccess: "caddy", PanelPortSet: true, PanelPort: 2096},
			want: func(t *testing.T, opts PromptOptions) {
				if opts.Domain != "example.com" {
					t.Fatalf("expected Domain example.com, got %q", opts.Domain)
				}
				if opts.Email != "admin@example.com" {
					t.Fatalf("expected Email admin@example.com, got %q", opts.Email)
				}
			},
			wantOutput: "Domain must not be empty",
		},
		{
			name: "caddy skips email prompt when already provided",
			in:   "example.com\n",
			opts: PromptOptions{PanelAccessSet: true, PanelAccess: "caddy", PanelPortSet: true, PanelPort: 2096, Email: "admin@example.com"},
			want: func(t *testing.T, opts PromptOptions) {
				if opts.Domain != "example.com" {
					t.Fatalf("expected Domain example.com, got %q", opts.Domain)
				}
				if opts.Email != "admin@example.com" {
					t.Fatalf("expected Email unchanged, got %q", opts.Email)
				}
			},
		},
		{
			name:        "error on access prompt",
			in:          "",
			opts:        PromptOptions{PanelPortSet: true},
			wantErr:     true,
			errContains: "EOF",
		},
		{
			name:        "error on port mode prompt",
			in:          "",
			opts:        PromptOptions{PanelAccessSet: true, PanelAccess: "local"},
			wantErr:     true,
			errContains: "EOF",
		},
		{
			name:        "error during custom port prompt",
			in:          "custom\n",
			opts:        PromptOptions{PanelAccessSet: true, PanelAccess: "direct"},
			wantErr:     true,
			errContains: "EOF",
		},
		{
			name:        "error on domain prompt",
			in:          "",
			opts:        PromptOptions{PanelAccessSet: true, PanelAccess: "caddy", PanelPortSet: true},
			wantErr:     true,
			errContains: "EOF",
		},
		{
			name:        "error on email prompt",
			in:          "example.com\n",
			opts:        PromptOptions{PanelAccessSet: true, PanelAccess: "caddy", PanelPortSet: true},
			wantErr:     true,
			errContains: "EOF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			opts := tt.opts
			err := NewPrompt(strings.NewReader(tt.in), &out).PromptMissingOptions(&opts)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error containing %q, got %v", tt.errContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.want != nil {
				tt.want(t, opts)
			}
			if tt.wantOutput != "" && !strings.Contains(out.String(), tt.wantOutput) {
				t.Fatalf("expected output to contain %q, got:\n%s", tt.wantOutput, out.String())
			}
		})
	}
}

func TestPromptPanelPortBranches(t *testing.T) {
	t.Run("rejects non-numeric and out-of-range then accepts valid", func(t *testing.T) {
		in := strings.NewReader("abc\n0\n70000\n3096\n")
		var out bytes.Buffer
		got, err := NewPrompt(in, &out).promptPanelPort(bufio.NewReader(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 3096 {
			t.Fatalf("expected port 3096, got %d", got)
		}
		outStr := out.String()
		if !strings.Contains(outStr, "Port must be a number") {
			t.Fatalf("expected non-numeric error message, got:\n%s", outStr)
		}
		if !strings.Contains(outStr, "Port must be between 1 and 65535") {
			t.Fatalf("expected out-of-range error message, got:\n%s", outStr)
		}
	})

	t.Run("returns read error", func(t *testing.T) {
		in := strings.NewReader("")
		var out bytes.Buffer
		_, err := NewPrompt(in, &out).promptPanelPort(bufio.NewReader(in))
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
}
