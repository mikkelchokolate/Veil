package naiveproxy

// RequiresRenderSettings reports that naiveproxy does not require
// generated render settings beyond the basic inbound data.
func (Plugin) RequiresRenderSettings() bool { return false }
