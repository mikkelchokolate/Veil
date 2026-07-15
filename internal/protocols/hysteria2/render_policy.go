package hysteria2

// RequiresRenderSettings reports that Hysteria2 needs generated render settings
// (domain, masquerade URL, password, upstream, etc.) to produce a valid config.
func (Plugin) RequiresRenderSettings() bool { return true }
