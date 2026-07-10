package runtime

type ManagedProcessPolicy struct {
	names []string
}

// NewManagedProcessPolicy returns the legacy fixed policy used when callers
// cannot reach the protocol registry. Production code should build the policy
// from the registry with NewManagedProcessPolicyFor so newly registered
// protocol plugins are recognized automatically.
func NewManagedProcessPolicy() ManagedProcessPolicy {
	return NewManagedProcessPolicyFor([]string{"caddy", "hysteria", "hysteria2", "mita", "mieru", "olcrtc", "sing-box", "veil"})
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

func isManagedProcess(name string) bool {
	return NewManagedProcessPolicy().IsManaged(name)
}
