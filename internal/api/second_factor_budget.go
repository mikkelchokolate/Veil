package api

import (
	"sync"
	"time"
)

// secondFactorGuessBudget is the process-wide number of second-factor
// guesses (TOTP or recovery code) one username may have outstanding across
// ALL clients. The per-(client, username) limiter, the login backoff and the
// per-challenge attempt cap are each scoped to a client /64 or a challenge,
// so an attacker who already holds the password could rotate prefixes (or a
// botnet) and re-mint challenges for a fresh guess budget each time — TOTP
// throughput then grew linearly with the number of sources (#1286). This
// budget is the 2FA-stage analogue of the password-stage
// delayGlobalUsernameAttempt (#1101), but over-budget guesses are REJECTED,
// not delayed: a sleep does not cap concurrent requests.
//
// At 20 guesses per hour the expected time to hit one of the three accepted
// codes in the ±1-step window (~333k guesses) is close to two years, while
// an operator mistyping a code a few times is never noticed. Only someone
// who passed the password stage can draw on the budget, so exhausting it
// does not let an anonymous caller lock the operator out.
const secondFactorGuessBudget = 20

// secondFactorGuessRefill is the time to regain one guess (20 per hour).
const secondFactorGuessRefill = 3 * time.Minute

// maxSecondFactorBudgetEntries bounds the budget map. Entries are only ever
// created after a correct password, so the map is naturally bounded by the
// number of real accounts; the cap is a backstop, and only entries that have
// fully refilled — indistinguishable from a missing entry — are pruned.
const maxSecondFactorBudgetEntries = 10000

type secondFactorBudgetState struct {
	tokens  float64
	updated time.Time
}

// reserveSecondFactorGuess atomically takes one guess from username's
// process-wide budget BEFORE the guess is evaluated, so concurrent guesses
// cannot overrun it. The caller must invoke refund when the attempt turned
// out not to be a wrong guess (success, account drift, server error); refund
// is idempotent. When the budget is exhausted ok is false and retryAfter is
// the time until the next guess becomes available.
func (s *managementState) reserveSecondFactorGuess(username string) (refund func(), retryAfter time.Duration, ok bool) {
	key := loginUsernameKey(username)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.loginBackoffTime()
	if s.secondFactorBudget == nil {
		s.secondFactorBudget = make(map[string]secondFactorBudgetState)
	}
	state, found := s.secondFactorBudget[key]
	if !found {
		state = secondFactorBudgetState{tokens: secondFactorGuessBudget, updated: now}
	}
	state = refillSecondFactorBudget(state, now)
	if state.tokens < 1 {
		s.secondFactorBudget[key] = state
		wait := time.Duration((1 - state.tokens) * float64(secondFactorGuessRefill))
		if wait <= 0 {
			wait = time.Second
		}
		return nil, wait, false
	}
	state.tokens--
	s.secondFactorBudget[key] = state
	if len(s.secondFactorBudget) > maxSecondFactorBudgetEntries {
		for candidate, item := range s.secondFactorBudget {
			if candidate != key && refillSecondFactorBudget(item, now).tokens >= secondFactorGuessBudget {
				delete(s.secondFactorBudget, candidate)
			}
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			current, ok := s.secondFactorBudget[key]
			if !ok {
				return
			}
			current = refillSecondFactorBudget(current, s.loginBackoffTime())
			current.tokens++
			if current.tokens > secondFactorGuessBudget {
				current.tokens = secondFactorGuessBudget
			}
			s.secondFactorBudget[key] = current
		})
	}, 0, true
}

func refillSecondFactorBudget(state secondFactorBudgetState, now time.Time) secondFactorBudgetState {
	if elapsed := now.Sub(state.updated); elapsed > 0 {
		state.tokens += float64(elapsed) / float64(secondFactorGuessRefill)
		if state.tokens > secondFactorGuessBudget {
			state.tokens = secondFactorGuessBudget
		}
		state.updated = now
	}
	return state
}
