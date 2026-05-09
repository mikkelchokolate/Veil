package runtime

import "testing"

func TestConnectionDiscoveryParsesHexAddress(t *testing.T) {
	addr, port := NewConnectionDiscovery().ParseHexAddress("0100007F:0830")
	if addr != "127.0.0.1" || port != 2096 {
		t.Fatalf("addr=%q port=%d", addr, port)
	}
}
