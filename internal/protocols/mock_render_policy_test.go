package protocols

func (m *mockConfigRenderer) RequiresRenderSettings() bool {
	return m.protocol != "mieru"
}
