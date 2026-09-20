package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadListeningSocketsParsesTCPFile(t *testing.T) {
	dir := t.TempDir()
	tcp := filepath.Join(dir, "tcp")
	content := "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n" +
		"0: 00000000:3039 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 111\n" +
		"1: 00000000:D431 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 222\n"
	if err := os.WriteFile(tcp, []byte(content), 0o644); err != nil {
		t.Fatalf("write tcp file: %v", err)
	}

	listeners, err := readListeningSockets(tcp, "tcp")
	if err != nil {
		t.Fatalf("readListeningSockets: %v", err)
	}
	if len(listeners) != 2 {
		t.Fatalf("listeners = %+v", listeners)
	}
	if listeners[0].Proto != "tcp" || listeners[0].Address != "0.0.0.0" || listeners[0].Port != 12345 {
		t.Fatalf("first listener = %+v", listeners[0])
	}
	if listeners[1].Proto != "tcp" || listeners[1].Address != "0.0.0.0" || listeners[1].Port != 54321 {
		t.Fatalf("second listener = %+v", listeners[1])
	}
}

// A connected socket whose remote port equals the listen port must not steal
// process attribution from the real listener (audit #335).
func TestFindInodeByPortPrefersListenRowOverConnectedRemotePort(t *testing.T) {
	lines := []string{
		"sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode",
		// connected outbound socket to :443 (remote port 01BB), listed first
		"0: 0100007F:C000 08080808:01BB 01 00000000:00000000 00:00000000 00000000 0 0 999",
		// the actual listener on :443
		"1: 00000000:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 111",
	}
	if inode := findInodeByPortInSocketLines("tcp", "01BB", lines); inode != "111" {
		t.Fatalf("inode = %q, want listener inode 111", inode)
	}
}

// For UDP the same attribution must ignore connected rows (non-zero remote).
func TestFindInodeByPortSkipsConnectedUDPRows(t *testing.T) {
	lines := []string{
		"sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode",
		// connected UDP socket locally bound to :40000 talking to a peer
		"0: 0100007F:9C40 08080808:9C40 07 00000000:00000000 00:00000000 00000000 0 0 999",
		// the real bound UDP listener on :40000
		"1: 00000000:9C40 00000000:0000 07 00000000:00000000 00:00000000 00000000 0 0 111",
	}
	if inode := findInodeByPortInSocketLines("udp", "9C40", lines); inode != "111" {
		t.Fatalf("inode = %q, want listener inode 111", inode)
	}
}
