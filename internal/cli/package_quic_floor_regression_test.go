package cli

import (
	"os"
	"strings"
	"testing"
)

// TestPostinstallQuicTuningOnlyRaises locks #1456 for the packaged
// maintainer script: `sysctl -p /etc/sysctl.d/99-veil-quic.conf` applied the
// shipped 16 MiB values unconditionally, so every package upgrade lowered a
// host tuned higher. The script now reads the live caps, rewrites the drop-in
// to at least the observed value and only raises what is below the floor.
func TestPostinstallQuicTuningOnlyRaises(t *testing.T) {
	body, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	code := stripHashComments(t, strings.ReplaceAll(string(body), "\r\n", "\n"))
	if strings.Contains(code, "sysctl -p /etc/sysctl.d/99-veil-quic.conf") {
		t.Fatalf("postinstall must not apply the drop-in unconditionally:\n%s", code)
	}
	for _, want := range []string{
		"/proc/sys/net/core/rmem_max",
		"/proc/sys/net/core/wmem_max",
		"quic_floor=16777216",
		"quic_explicit_max net.core.rmem_max",
		"quic_explicit_max net.core.wmem_max",
		"/etc/sysctl.conf",
		`sysctl -w "net.core.rmem_max=${quic_rmem}"`,
		`sysctl -w "net.core.wmem_max=${quic_wmem}"`,
	} {
		if !strings.Contains(code, want) {
			t.Fatalf("postinstall is missing the only-raise step %q", want)
		}
	}
}
