package runtime

import (
	sysruntime "runtime"
	"testing"
)

func TestRuntimeProcFSExposesRuntimeReaders(t *testing.T) {
	if sysruntime.GOOS == "windows" {
		t.Skip("Skipping ProcFS tests on Windows since it has no /proc")
	}
	procfs := NewRuntimeProcFS()
	if _, err := procfs.System(); err != nil {
		t.Fatalf("System: %v", err)
	}
	if _, err := procfs.Network(); err != nil {
		t.Fatalf("Network: %v", err)
	}
}
