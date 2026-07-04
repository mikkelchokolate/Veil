package firewall

import "testing"

func TestNewStatusReaderFallsBackToDefaultRunnerOnNil(t *testing.T) {
	reader := NewStatusReader(nil)
	if reader.runner == nil {
		t.Fatal("expected non-nil runner when nil is passed")
	}
}
