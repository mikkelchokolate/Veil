package installer

import "testing"

func TestHysteria2ReleaseAssetURL(t *testing.T) {
	url, err := Hysteria2ReleaseAssetURL("v2.6.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://github.com/apernet/hysteria/releases/download/app%2Fv2.6.0/hysteria-linux-amd64"
	if url != want {
		t.Fatalf("unexpected url:\n got: %s\nwant: %s", url, want)
	}
}

func TestHysteria2ReleaseAssetURLRejectsUnsupportedArch(t *testing.T) {
	_, err := Hysteria2ReleaseAssetURL("v2.6.0", "linux", "mips")
	if err == nil {
		t.Fatalf("expected unsupported arch error")
	}
}

func TestVerifySHA256HexAcceptsMatchingHash(t *testing.T) {
	got, err := SHA256Hex([]byte("veil"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "01979b66ee1794c473f53cafff0889e714aac28b2515e3572072424b919634f3" {
		t.Fatalf("unexpected sha256: %s", got)
	}
	if err := VerifySHA256Hex([]byte("veil"), got); err != nil {
		t.Fatalf("expected matching hash: %v", err)
	}
}

func TestVerifySHA256HexRejectsMismatch(t *testing.T) {
	if err := VerifySHA256Hex([]byte("veil"), "deadbeef"); err == nil {
		t.Fatalf("expected mismatch error")
	}
}

func TestCaddyNaiveBuildHint(t *testing.T) {
	hint := CaddyNaiveBuildHint("/usr/local/bin/caddy")
	if hint.BinaryPath != "/usr/local/bin/caddy" {
		t.Fatalf("unexpected binary path: %+v", hint)
	}
	if len(hint.Commands) == 0 {
		t.Fatalf("expected commands")
	}
}
