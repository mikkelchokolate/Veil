package renderer

import (
	"strings"
	"testing"
)

func TestRenderMieruRejectsInvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RenderMieru(MieruConfig{
				PortBindings: []MieruPortBinding{{Port: tt.port, Protocol: "tcp"}},
				Users:        []MieruUser{{Name: "alice", Password: "secret"}},
			})
			if err == nil {
				t.Fatal("expected error for invalid port")
			}
			if !strings.Contains(err.Error(), "mieru port is required") {
				t.Fatalf("expected port error, got: %v", err)
			}
		})
	}
}

func TestRenderMieruRejectsInvalidProtocol(t *testing.T) {
	_, err := RenderMieru(MieruConfig{
		PortBindings: []MieruPortBinding{{Port: 443, Protocol: "icmp"}},
		Users:        []MieruUser{{Name: "alice", Password: "secret"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid protocol")
	}
	if !strings.Contains(err.Error(), "mieru protocol must be TCP or UDP") {
		t.Fatalf("expected protocol error, got: %v", err)
	}
}

func TestRenderMieruRejectsEmptyUserCredentials(t *testing.T) {
	tests := []struct {
		name string
		user MieruUser
	}{
		{"empty name", MieruUser{Name: "", Password: "secret"}},
		{"empty password", MieruUser{Name: "alice", Password: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RenderMieru(MieruConfig{
				PortBindings: []MieruPortBinding{{Port: 443, Protocol: "tcp"}},
				Users:        []MieruUser{tt.user},
			})
			if err == nil {
				t.Fatal("expected error for empty user credential")
			}
			if !strings.Contains(err.Error(), "mieru user name and password are required") {
				t.Fatalf("expected user error, got: %v", err)
			}
		})
	}
}
