package runtime

type ManagedProcessPolicy struct {
	names []string
}

func NewManagedProcessPolicy() ManagedProcessPolicy {
	return ManagedProcessPolicy{names: []string{"caddy", "hysteria2", "sing-box", "veil", "mieru", "olcrtc"}}
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
