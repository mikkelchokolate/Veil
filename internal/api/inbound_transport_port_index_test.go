package api

import "testing"

func TestInboundTransportPortIndexDetectsDuplicatesExceptIndex(t *testing.T) {
	index := NewInboundTransportPortIndex([]Inbound{
		{Name: "a", Transport: "tcp", Port: 443},
		{Name: "b", Transport: "udp", Port: 443},
	})
	if !index.Has("tcp", 443, -1) {
		t.Fatal("expected tcp/443 duplicate")
	}
	if index.Has("tcp", 443, 0) {
		t.Fatal("expected tcp/443 ignored at except index")
	}
	if index.Has("tcp", 443, 1) != true {
		t.Fatal("udp/443 must not hide tcp/443 duplicate")
	}
	if index.Has("udp", 443, 0) != true {
		t.Fatal("tcp/443 must not hide udp/443 duplicate")
	}
	if index.Has("udp", 8443, -1) {
		t.Fatal("did not expect udp/8443 duplicate")
	}
}
