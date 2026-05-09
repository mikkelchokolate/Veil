package runtime

import "testing"

func TestConnectionProcSocketPathBuildsKernelSocketTablePaths(t *testing.T) {
	path := NewConnectionProcSocketPath()
	if got := path.ForProtocol("tcp"); got != "/proc/net/tcp" {
		t.Fatalf("tcp path = %q", got)
	}
	if got := path.ForProtocol("udp"); got != "/proc/net/udp" {
		t.Fatalf("udp path = %q", got)
	}
	if got := path.ForProtocol("raw"); got != "/proc/net/raw" {
		t.Fatalf("raw path = %q", got)
	}
}
