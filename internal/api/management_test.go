package api

import "testing"

func TestApplyHistoryStageReturnsCorrectStage(t *testing.T) {
	tests := []struct {
		name     string
		response ApplyResponse
		want     string
	}{
		{
			name:     "rollback stage",
			response: ApplyResponse{RolledBack: true},
			want:     "rollback",
		},
		{
			name:     "services stage supersedes live",
			response: ApplyResponse{ServicesApplied: true, LiveApplied: true},
			want:     "services",
		},
		{
			name:     "live stage",
			response: ApplyResponse{LiveApplied: true},
			want:     "live",
		},
		{
			name:     "staged fallback",
			response: ApplyResponse{},
			want:     "staged",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyHistoryStage(tt.response)
			if got != tt.want {
				t.Fatalf("applyHistoryStage() = %q, want %q", got, tt.want)
			}
		})
	}
}
