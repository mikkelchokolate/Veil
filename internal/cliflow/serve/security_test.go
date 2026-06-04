package serve

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestServeSecurityResolvesSafeConfig(t *testing.T) {
	security := NewSecurity(SecurityOptions{
		Listen:      "127.0.0.1:2096",
		AuthToken:   "secret",
		StatePath:   "/state.json",
		ApplyRoot:   "/apply",
		KeyPath:     "/state.key",
		WebBasePath: "secret",
	})
	cfg, err := security.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Token != "secret" || cfg.TokenSource != "--auth-token" || cfg.WebBasePath != "/secret/" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestServeSecurityRejectsPublicListenWithoutSessionUsersEvenWithToken(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	security := NewSecurity(SecurityOptions{
		Listen:    "0.0.0.0:2096",
		AuthToken: "secret",
		StatePath: statePath,
	})

	_, err := security.Resolve()
	if err == nil {
		t.Fatalf("expected public listen without session users to be rejected")
	}
	if !strings.Contains(err.Error(), "veil admin reset") || !strings.Contains(err.Error(), "session") {
		t.Fatalf("expected first-run session setup guidance, got %v", err)
	}
}

func TestServeSecurityAllowsPublicListenWithTokenAndSessionUser(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := managementstate.NewStore(statePath, nil)
	if err := store.Save(model.ManagementSnapshot{
		Settings: model.Settings{PanelListen: "0.0.0.0:2096", Mode: "server"},
		Users: []model.User{
			{Username: "admin", PasswordHash: "hash", Role: "admin"},
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	security := NewSecurity(SecurityOptions{
		Listen:    "0.0.0.0:2096",
		AuthToken: "secret",
		StatePath: statePath,
	})

	cfg, err := security.Resolve()
	if err != nil {
		t.Fatalf("expected public listen with token and session user to be allowed: %v", err)
	}
	if !cfg.PublicListen || !cfg.SessionAuthConfigured || !cfg.MetricsAuthRequired {
		t.Fatalf("expected public/session/metrics auth flags, got %+v", cfg)
	}
}
