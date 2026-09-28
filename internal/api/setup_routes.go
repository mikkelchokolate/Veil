package api

import (
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/panel"
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
	Locale             string `json:"locale,omitempty"`
}

func (s *managementState) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// required asks "can a client perform first-run setup now?". Once the
	// instance has ever been provisioned the answer stays no even if a
	// rollback or restore emptied the user list — re-running setup would be
	// the anonymous-admin regrant of #1100. Recovery is `veil admin reset`.
	required := !s.setup.Completed && len(s.users) == 0 && !s.usersProvisionedLocked()
	// Settings may not have been normalized yet (e.g. before first save); an
	// unset panelAccess behaves identically to "local" everywhere else, so
	// report the effective mode instead of leaking the empty sentinel.
	panelAccess := s.settings.PanelAccess
	if panelAccess == "" {
		panelAccess = "local"
	}
	writeJSON(w, SetupStatusResponse{
		Required:    required,
		Allowed:     s.setupAllowed && required,
		PanelAccess: panelAccess,
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
		writeError(w, "username must be 3-64 characters and at most 64 UTF-8 bytes, using letters, digits, dot, underscore, or hyphen", http.StatusBadRequest)
		return
	}
	if err := validatePanelPassword(req.Password); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !req.BackupAcknowledged {
		writeError(w, "backup and recovery acknowledgement is required", http.StatusBadRequest)
		return
	}
	if req.Locale != "" {
		locale, ok := panel.ParseLocale(req.Locale)
		if !ok {
			writeError(w, "locale must be en or ru", http.StatusBadRequest)
			return
		}
		req.Locale = locale
	} else {
		req.Locale = panel.ResolveLocale("", r)
	}
	// Cheap pre-check before spending a bcrypt hash: an already-provisioned
	// instance (the overwhelmingly common case) gets its conflict answer
	// without queueing CPU-bound hash work at all (#1101). The authoritative
	// check is repeated under the lock below.
	s.mu.Lock()
	alreadyDone := s.setup.Completed || len(s.users) != 0 || s.usersProvisionedLocked()
	s.mu.Unlock()
	if alreadyDone {
		writeError(w, "first-run setup is already complete", http.StatusConflict)
		return
	}

	// Setup hashing shares the login bcrypt ceiling so a setup/login spray
	// cannot stack unbounded CPU-bound work (#1101).
	releaseBcrypt := acquireBcryptWork()
	if releaseBcrypt == nil {
		w.Header().Set("Retry-After", "1")
		writeError(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	hashed, hashErr := s.hashPassword([]byte(req.Password))
	releaseBcrypt()
	if hashErr != nil {
		writeError(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// The provisioning latch must agree setup is still possible: a
	// provisioned-then-emptied instance reports "already complete" so the
	// anonymous first-run path can never re-arm (#1100).
	if s.setup.Completed || len(s.users) != 0 || s.usersProvisionedLocked() {
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
		Locale:       req.Locale,
	}}
	// The first user permanently latches the instance as provisioned —
	// outside the state file itself, so no rollback/restore can clear it —
	// and revokes any bootstrap sessions minted before this point (#1100).
	s.noteUsersProvisionedLocked()
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
		"locale":    req.Locale,
	})
}

func validSetupUsername(username string) bool {
	if utf8.RuneCountInString(username) < 3 || len(username) > 64 {
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
