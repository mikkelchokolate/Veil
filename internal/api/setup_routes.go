package api

import (
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"golang.org/x/crypto/bcrypt"
)

type SetupStatusResponse struct {
	Required    bool   `json:"required"`
	Allowed     bool   `json:"allowed"`
	PanelAccess string `json:"panelAccess"`
}

type setupCompleteRequest struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	BackupAcknowledged bool   `json:"backupAcknowledged"`
}

func (s *managementState) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	required := !s.setup.Completed && len(s.users) == 0
	writeJSON(w, SetupStatusResponse{
		Required:    required,
		Allowed:     s.setupAllowed && required,
		PanelAccess: s.settings.PanelAccess,
	})
}

func (s *managementState) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.setupAllowed {
		writeError(w, "first-run setup is available only on a local loopback Panel", http.StatusForbidden)
		return
	}

	var req setupCompleteRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !validSetupUsername(req.Username) {
		writeError(w, "username must be 3-64 characters using letters, digits, dot, underscore, or hyphen", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 12 {
		writeError(w, "password must be at least 12 characters", http.StatusBadRequest)
		return
	}
	if !req.BackupAcknowledged {
		writeError(w, "backup and recovery acknowledgement is required", http.StatusBadRequest)
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setup.Completed || len(s.users) != 0 {
		writeError(w, "first-run setup is already complete", http.StatusConflict)
		return
	}

	previousSetup := s.setup
	previousUsers := append([]User(nil), s.users...)
	s.setup = SetupState{
		Completed:   true,
		CompletedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.users = []User{{
		Username:     req.Username,
		PasswordHash: string(hashed),
		Role:         "admin",
	}}
	if err := s.saveLocked(); err != nil {
		s.setup = previousSetup
		s.users = previousUsers
		writeError(w, "failed to persist first-run setup", http.StatusInternalServerError)
		return
	}
	s.recordRequestAudit(r, audit.Record{
		Actor:   req.Username,
		Role:    "admin",
		Action:  "setup.complete",
		Target:  "panel",
		Success: true,
	})

	writeJSONStatus(w, http.StatusCreated, map[string]any{
		"completed": true,
		"username":  req.Username,
		"role":      "admin",
	})
}

func validSetupUsername(username string) bool {
	if len(username) < 3 || len(username) > 64 {
		return false
	}
	for _, r := range username {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
