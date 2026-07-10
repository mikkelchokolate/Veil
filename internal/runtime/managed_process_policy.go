package runtime

// ManagedProcessPolicy filters process names for managed service discovery.
type ManagedProcessPolicy struct {
	names []string
}

// NewManagedProcessPolicyFor creates a policy from the supplied process names.
// Callers that have access to the protocol registry should derive the names
// from RuntimeProvider.RuntimeInstall so the list stays in sync with the
// installed protocol plugins.
func NewManagedProcessPolicyFor(names []string) ManagedProcessPolicy {
	out := make([]string, len(names))
	copy(out, names)
	return ManagedProcessPolicy{names: out}
}

func (p ManagedProcessPolicy) IsManaged(name string) bool {
	for _, managed := range p.names {
		if name == managed {
			return true
		}
	}
	return false
}
