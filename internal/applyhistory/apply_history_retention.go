package applyhistory

type ApplyHistoryRetention struct {
	max int
}

func NewApplyHistoryRetention(max int) ApplyHistoryRetention {
	if max <= 0 {
		max = MaxEntries
	}
	return ApplyHistoryRetention{max: max}
}

func (r ApplyHistoryRetention) Max() int { return r.max }

func (r ApplyHistoryRetention) Prepend(entry ApplyHistoryEntry, history []ApplyHistoryEntry) []ApplyHistoryEntry {
	kept := append([]ApplyHistoryEntry{entry}, history...)
	if len(kept) > r.max {
		kept = kept[:r.max]
	}
	return kept
}
