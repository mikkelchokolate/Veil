package api

import "time"

// loginBackoffStaleAfter is the horizon past which an idle (client, username)
// key resets: its failure count is dropped on the next touch and it is the
// first eviction class when the map overflows (#1222).
const loginBackoffStaleAfter = 15 * time.Minute

// loginBackoffMaxEntries bounds the backoff map. Overflow eviction removes
// strictly-stale entries first, then the oldest lastSeen — random map order
// must never pick the victim, or a spray would reset fresh backoff state for
// unrelated keys (#1222).
const loginBackoffMaxEntries = 10000

type loginBackoffState struct {
	failures int
	nextTry  time.Time
	lastSeen time.Time
}

func (s *managementState) loginBackoffRemaining(key string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.loginBackoff[key]
	now := s.loginBackoffTime()
	if state.nextTry.After(now) {
		return state.nextTry.Sub(now)
	}
	return 0
}

func (s *managementState) recordLoginFailure(key string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loginBackoff == nil {
		s.loginBackoff = make(map[string]loginBackoffState)
	}
	now := s.loginBackoffTime()
	state := s.loginBackoff[key]
	if now.Sub(state.lastSeen) > loginBackoffStaleAfter {
		state.failures = 0
	}
	state.failures++
	exponent := state.failures - 1
	if exponent > 3 {
		exponent = 3
	}
	delay := time.Second * time.Duration(1<<exponent)
	state.nextTry = now.Add(delay)
	state.lastSeen = now
	s.loginBackoff[key] = state
	if len(s.loginBackoff) > loginBackoffMaxEntries {
		// Evict strictly-stale entries first, then the oldest lastSeen —
		// never a random victim, which could be fresh backoff state for an
		// unrelated (client, username) key.
		for candidate, item := range s.loginBackoff {
			if candidate == key {
				continue
			}
			if now.Sub(item.lastSeen) > loginBackoffStaleAfter {
				delete(s.loginBackoff, candidate)
			}
		}
		for len(s.loginBackoff) > loginBackoffMaxEntries {
			oldestKey, oldest := "", time.Time{}
			for candidate, item := range s.loginBackoff {
				if candidate == key {
					continue
				}
				if oldestKey == "" || item.lastSeen.Before(oldest) {
					oldestKey, oldest = candidate, item.lastSeen
				}
			}
			if oldestKey == "" {
				break
			}
			delete(s.loginBackoff, oldestKey)
		}
	}
	return delay
}

func (s *managementState) clearLoginFailures(key string) {
	s.mu.Lock()
	delete(s.loginBackoff, key)
	s.mu.Unlock()
}

func (s *managementState) loginBackoffTime() time.Time {
	if s.loginBackoffNow != nil {
		return s.loginBackoffNow()
	}
	return time.Now()
}
