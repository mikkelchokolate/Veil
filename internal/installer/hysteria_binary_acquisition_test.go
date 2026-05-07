package installer

import "testing"

func TestHysteriaBinaryAcquisitionBuildsReleaseAssetAndChecksum(t *testing.T) {
	artifact, err := NewHysteriaBinaryAcquisition().Build("v2.6.0", "linux", "amd64", " abc123 ")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if artifact.URL == "" || artifact.Binary.URL != artifact.URL {
		t.Fatalf("artifact = %+v", artifact)
	}
	if artifact.Binary.Name != "hysteria2" || artifact.Binary.Destination != "/usr/local/bin/hysteria" || artifact.Binary.SHA256 != "abc123" {
		t.Fatalf("binary = %+v", artifact.Binary)
	}
}
