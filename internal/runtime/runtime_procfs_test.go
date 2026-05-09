package runtime

import "testing"

func TestRuntimeProcFSExposesRuntimeReaders(t *testing.T) {
	procfs := NewRuntimeProcFS()
	if _, err := procfs.System(); err != nil {
		t.Fatalf("System: %v", err)
	}
	if _, err := procfs.Network(); err != nil {
		t.Fatalf("Network: %v", err)
	}
}
