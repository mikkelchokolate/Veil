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
