package api

import "time"

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
	if now.Sub(state.lastSeen) > 15*time.Minute {
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
	if len(s.loginBackoff) > 10000 {
		for candidate, item := range s.loginBackoff {
			if now.Sub(item.lastSeen) > 15*time.Minute || candidate != key {
				delete(s.loginBackoff, candidate)
				if len(s.loginBackoff) <= 10000 {
					break
				}
			}
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
