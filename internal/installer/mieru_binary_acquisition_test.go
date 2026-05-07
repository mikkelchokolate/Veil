package installer

import "testing"

func TestMieruBinaryAcquisitionBuildsGitHubReleaseAsset(t *testing.T) {
	artifact, err := NewMieruBinaryAcquisition().Build("v3.12.0", "linux", "amd64", " abc123 ")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	wantURL := "https://github.com/enfein/mieru/releases/download/v3.12.0/mieru_3.12.0_linux_amd64.tar.gz"
	if artifact.URL != wantURL || artifact.Binary.URL != wantURL {
		t.Fatalf("artifact = %+v", artifact)
	}
	if artifact.Binary.Name != "mieru" || artifact.Binary.Destination != "/usr/local/bin/mieru" || artifact.Binary.SHA256 != "abc123" {
		t.Fatalf("binary = %+v", artifact.Binary)
	}
}

func TestMieruBinaryAcquisitionNormalizesArch(t *testing.T) {
	artifact, err := NewMieruBinaryAcquisition().Build("v3.12.0", "linux", "x86_64", "")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if artifact.Binary.URL != "https://github.com/enfein/mieru/releases/download/v3.12.0/mieru_3.12.0_linux_amd64.tar.gz" {
		t.Fatalf("url = %q", artifact.Binary.URL)
	}
}
