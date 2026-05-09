package api

type ApplyHistory struct {
	store ApplyHistoryStore
}

func NewApplyHistory(path string, max int) ApplyHistory {
	return ApplyHistory{store: NewApplyHistoryStore(path, max)}
}

func (h ApplyHistory) Load() ([]ApplyHistoryEntry, error) {
	return h.store.Load()
}

func (h ApplyHistory) Append(stage string, success bool, response ApplyResponse) error {
	return h.store.Append(stage, success, response)
}

func (h ApplyHistory) Query(values map[string][]string) ([]ApplyHistoryEntry, error) {
	history, err := h.Load()
	if err != nil {
		return nil, err
	}
	return NewApplyHistoryFilter(values).Apply(history)
}
