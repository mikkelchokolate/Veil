package api

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The "users ever existed" latch. Loopback dev-anonymous admin exists so a
// never-configured local instance can complete first-run setup — but the
// check historically only looked at the CURRENT users list, so any later
// state that dropped users back to zero (a rollback to a pre-setup revision,
// a backup restore of a zero-user snapshot, deleting all users) silently
// re-opened unauthenticated admin (#1100). Once a user row exists the
// instance is provisioned forever: this marker is persisted next to
// state.json — outside every snapshot-managed field — so no rollback or
// restore can clear it, and a zero-user state thereafter fails closed.
// Recovery from that state is the documented CLI path (`veil admin reset` /
// `veil admin set`), which writes a user into state.json directly.
const usersProvisionedMarkerName = "users-provisioned.marker"

func (s *managementState) usersProvisionedMarkerPath() string {
	statePath := strings.TrimSpace(s.statePath)
	if statePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(statePath), usersProvisionedMarkerName)
}

// usersProvisionedLocked reports whether this instance has ever held a panel
// user. Caller must hold s.mu. True whenever a user row currently exists;
// once true it stays true for the life of the data directory.
func (s *managementState) usersProvisionedLocked() bool {
	if len(s.users) > 0 || s.usersEverExisted {
		return true
	}
	marker := s.usersProvisionedMarkerPath()
	if marker == "" {
		return false
	}
	if _, err := os.Stat(marker); err == nil {
		// Re-adopt the persisted latch after restart (or after state was
		// replaced underneath a running instance).
		s.usersEverExisted = true
		s.usersProvisionedMarkerWritten = true
		return true
	}
	return false
}

// noteUsersProvisionedLocked is called by every path that can leave
// s.users non-empty (mutations, setup, snapshot apply, rollback, restore).
// It latches the instance as provisioned, writes the durable marker once,
// and revokes every fallback-minted bootstrap session: sessions created
// without a real credential must not survive the appearance of the account
// they mimicked (#1112). Caller must hold s.mu.
func (s *managementState) noteUsersProvisionedLocked() {
	if len(s.users) == 0 {
		return
	}
	firstObservation := !s.usersEverExisted
	s.usersEverExisted = true
	if !s.usersProvisionedMarkerWritten {
		if marker := s.usersProvisionedMarkerPath(); marker != "" {
			body := []byte("# Veil provisioning latch: a panel user existed on this instance.\n" +
				"# Unauthenticated loopback access stays disabled even if users are removed later.\n" +
				time.Now().UTC().Format(time.RFC3339) + "\n")
			// A failed marker write is non-fatal: the in-memory latch holds
			// for this process and the next user-bearing load retries it.
			if err := writeSessionFile(marker, body); err == nil {
				s.usersProvisionedMarkerWritten = true
			}
		}
	}
	if firstObservation && s.sessions != nil {
		// Bootstrap sessions are only meaningful while zero users exist; the
		// moment any account is created they must die, even when the new
		// account coincidentally shares their username (#1112). Only this
		// state's own registry is swept — never the shared globalSessions
		// fallback that bare test-constructed states resolve to.
		_, _ = s.sessions.DeleteBootstrapPersisted()
	}
}

// provisionedRecoveryHint is appended to the 401 body emitted when a
// provisioned-then-emptied instance rejects anonymous access, pointing the
// operator at the documented CLI recovery path instead of a dead login page.
const provisionedRecoveryHint = "run `veil admin reset` or `veil admin set` on the host to restore access"

// isProvisionedAuthLockdownLocked reports the specific fail-closed state the
// recovery hint applies to: the instance was provisioned but currently holds
// zero users, so first-run paths (dev-anonymous, NaivePassword fallback,
// setup) all stay closed.
func (s *managementState) isProvisionedAuthLockdownLocked() bool {
	return len(s.users) == 0 && s.usersProvisionedLocked()
}
