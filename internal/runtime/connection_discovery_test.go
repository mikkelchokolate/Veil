package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConnectionDiscoveryParsesHexAddress(t *testing.T) {
	addr, port := NewConnectionDiscovery().ParseHexAddress("0100007F:0830")
	if addr != "127.0.0.1" || port != 2096 {
		t.Fatalf("addr=%q port=%d", addr, port)
	}
}

func TestConnectionDiscoveryReadListeningSocketsFromTempFile(t *testing.T) {
	dir := t.TempDir()
	tcp := filepath.Join(dir, "tcp")
	content := "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n" +
		"0: 0100007F:0830 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 111\n"
	if err := os.WriteFile(tcp, []byte(content), 0o644); err != nil {
		t.Fatalf("write tcp file: %v", err)
	}

	listeners, err := NewConnectionDiscovery().ReadListeningSockets(tcp, "tcp")
	if err != nil {
		t.Fatalf("ReadListeningSockets: %v", err)
	}
	if len(listeners) != 1 {
		t.Fatalf("listeners = %+v", listeners)
	}
	if listeners[0].Proto != "tcp" || listeners[0].Address != "127.0.0.1" || listeners[0].Port != 2096 {
		t.Fatalf("listener = %+v", listeners[0])
	}
}

func TestConnectionDiscoveryListeningSocketsPropagatesReadError(t *testing.T) {
	source := fakeConnectionSource{err: errors.New("read failed")}
	_, err := newConnectionDiscoveryWithSource(source).listeningSockets("tcp")
	if err == nil || err.Error() != "read failed" {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestFileConnectionSourceReadsSocketLinesFromPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tcp6")
	content := "header\n0: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 0A ...\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	source := fileConnectionSource{path: path}
	lines, err := source.SocketLines("tcp6")
	if err != nil {
		t.Fatalf("SocketLines: %v", err)
	}
	if len(lines) != 2 || lines[0] != "header" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestReadLinesReturnsErrorForMissingFile(t *testing.T) {
	_, err := readLines("/nonexistent/veil-runtime-test-path")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadLinesReadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	lines, err := readLines(path)
	if err != nil {
		t.Fatalf("readLines: %v", err)
	}
	if len(lines) != 3 || lines[2] != "three" {
		t.Fatalf("lines = %+v", lines)
	}
}
