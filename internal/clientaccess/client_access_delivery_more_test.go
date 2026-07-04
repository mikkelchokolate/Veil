package clientaccess

import "testing"

func TestClientAccessDeliveryArtifactFilenameFallsBackToProtocol(t *testing.T) {
	filename := clientArtifactFilename(ClientLink{Name: "", Protocol: "mieru"})
	if filename != "mieru.json" {
		t.Fatalf("filename = %q, want mieru.json", filename)
	}
}
