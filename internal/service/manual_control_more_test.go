package service

import (
	"errors"
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type fakeManualControlRunner2 struct {
	outs []veilruntime.RuntimeCommandOutput
}

func (r *fakeManualControlRunner2) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	if len(r.outs) == 0 {
		return veilruntime.RuntimeCommandOutput{}
	}
	out := r.outs[0]
	r.outs = r.outs[1:]
	return out
}

func TestNewManualServiceControlDefaultsRunner(t *testing.T) {
	control := NewManualServiceControl(NewManagedRuntimeCatalog(nil), nil)
	if control.runner == nil {
		t.Fatal("expected default runner when nil is passed")
	}
}

func TestManualServiceControlAllows(t *testing.T) {
	control := NewManualServiceControl(NewManagedRuntimeCatalog([]ManagedRuntime{
		{ActionName: "mieru", Unit: "veil-mieru.service", ManualRestart: true},
	}), nil)
	if !control.Allows("mieru") {
		t.Fatal("expected Allows to return true")
	}
	if control.Allows("caddy") {
		t.Fatal("expected Allows to return false")
	}
}

func TestManualServiceControlRunBranches(t *testing.T) {
	control := NewManualServiceControl(NewManagedRuntimeCatalog([]ManagedRuntime{
		{ActionName: "mieru", Unit: "veil-mieru.service", ManualRestart: true},
	}), nil)

	tests := []struct {
		name        string
		outs        []veilruntime.RuntimeCommandOutput
		wantSuccess bool
		wantError   string
	}{
		{
			name:        "not found",
			outs:        []veilruntime.RuntimeCommandOutput{{NotFound: true, Err: errors.New("systemctl not found")}},
			wantSuccess: false,
			wantError:   "systemctl not found",
		},
		{
			name:        "timed out",
			outs:        []veilruntime.RuntimeCommandOutput{{TimedOut: true, Err: errors.New("context deadline exceeded")}},
			wantSuccess: false,
			wantError:   "service action timed out",
		},
		{
			name:        "success",
			outs:        []veilruntime.RuntimeCommandOutput{{Output: "restarted"}},
			wantSuccess: true,
			wantError:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			control.runner = &fakeManualControlRunner2{outs: tt.outs}
			result := control.Run("mieru", "restart")
			if result.Success != tt.wantSuccess {
				t.Fatalf("Success = %v, want %v", result.Success, tt.wantSuccess)
			}
			if result.Error != tt.wantError {
				t.Fatalf("Error = %q, want %q", result.Error, tt.wantError)
			}
		})
	}
}
