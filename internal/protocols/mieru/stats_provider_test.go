package mieru

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func stubMitaExec(t *testing.T, output []byte, err error) {
	t.Helper()
	original := execGetMetrics
	execGetMetrics = func(ctx context.Context, mitaPath, sockPath string) ([]byte, error) {
		return output, err
	}
	t.Cleanup(func() { execGetMetrics = original })
}

func TestStatsProviderMapsUserMetricsToBindings(t *testing.T) {
	stubMitaExec(t, []byte(`{
    "kcp": {"ActiveConnections": 1},
    "traffic": {"DownloadBytes": 999, "UploadBytes": 888},
    "users": {
        "alice": {"DownloadBytes": 100, "UploadBytes": 50},
        "v_9f2c": {"DownloadBytes": 30, "UploadBytes": 10},
        "bob": {"DownloadBytes": 5, "UploadBytes": 7},
        "stray-user": {"DownloadBytes": 1, "UploadBytes": 2}
    }
}`), nil)
	provider := NewStatsProvider("mieru:server", map[string]string{
		"alice":  "binding-1",
		"v_9f2c": "binding-1", // migrated client alias and runtime identity merge
		"bob":    "binding-2",
	})
	batch, err := provider.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if batch.RuntimeInstance != "mieru:server" {
		t.Fatalf("RuntimeInstance = %q", batch.RuntimeInstance)
	}
	if len(batch.Readings) != 2 {
		t.Fatalf("readings = %+v", batch.Readings)
	}
	first, second := batch.Readings[0], batch.Readings[1]
	if first.BindingID != "binding-1" || first.UploadBytes != 60 || first.DownloadBytes != 130 {
		t.Fatalf("binding-1 merged counters = %+v", first)
	}
	if second.BindingID != "binding-2" || second.UploadBytes != 7 || second.DownloadBytes != 5 {
		t.Fatalf("binding-2 counters = %+v", second)
	}
	if len(batch.UnknownIdentities) != 1 || batch.UnknownIdentities[0] != "stray-user" {
		t.Fatalf("unknown identities = %v", batch.UnknownIdentities)
	}
	if batch.ObservedAt.IsZero() {
		t.Fatal("ObservedAt not set")
	}
}

func TestStatsProviderToleratesLogPrefixAndTrailingNoise(t *testing.T) {
	stubMitaExec(t, []byte("INFO 2026-09-18 12:00:00 starting query\n{\"users\": {\"u\": {\"DownloadBytes\": 4, \"UploadBytes\": 6}}}\nINFO done\n"), nil)
	provider := NewStatsProvider("mieru:server", map[string]string{"u": "b1"})
	batch, err := provider.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(batch.Readings) != 1 || batch.Readings[0].UploadBytes != 6 || batch.Readings[0].DownloadBytes != 4 {
		t.Fatalf("readings = %+v", batch.Readings)
	}
}

func TestStatsProviderHealthyWhenNoUsersReportedYet(t *testing.T) {
	stubMitaExec(t, []byte(`{"traffic": {"DownloadBytes": 0, "UploadBytes": 0}}`), nil)
	provider := NewStatsProvider("mieru:server", map[string]string{"alice": "b1"})
	batch, err := provider.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(batch.Readings) != 0 || len(batch.UnknownIdentities) != 0 {
		t.Fatalf("batch = %+v", batch)
	}
}

func TestStatsProviderPropagatesExecFailure(t *testing.T) {
	stubMitaExec(t, nil, errors.New("mita get metrics: exit status 1: server is not running"))
	provider := NewStatsProvider("mieru:server", map[string]string{"alice": "b1"})
	if _, err := provider.Read(); err == nil || !strings.Contains(err.Error(), "server is not running") {
		t.Fatalf("err = %v", err)
	}
}

func TestStatsProviderRejectsMalformedJSON(t *testing.T) {
	for _, output := range [][]byte{
		[]byte("not json at all"),
		[]byte("{\"users\": {\"u\": {"),
		[]byte("prefix only, no object"),
	} {
		t.Run(string(output[:12]), func(t *testing.T) {
			stubMitaExec(t, output, nil)
			stub := NewStatsProvider("mieru:server", map[string]string{"u": "b1"})
			if _, err := stub.Read(); err == nil {
				t.Fatalf("expected error for output %q", output)
			}
		})
	}
}

func TestStatsProviderRejectsNegativeCounter(t *testing.T) {
	stubMitaExec(t, []byte(`{"users": {"u": {"DownloadBytes": -1, "UploadBytes": 0}}}`), nil)
	provider := NewStatsProvider("mieru:server", map[string]string{"u": "b1"})
	if _, err := provider.Read(); err == nil || !strings.Contains(err.Error(), "negative counter") {
		t.Fatalf("err = %v", err)
	}
}

func TestStatsProviderReportsAllUsersUnknownWithoutBindings(t *testing.T) {
	stubMitaExec(t, []byte(`{"users": {"z": {"DownloadBytes": 1, "UploadBytes": 1}, "a": {"DownloadBytes": 2, "UploadBytes": 2}}}`), nil)
	provider := NewStatsProvider("mieru:server", nil)
	batch, err := provider.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(batch.Readings) != 0 {
		t.Fatalf("readings = %+v", batch.Readings)
	}
	if len(batch.UnknownIdentities) != 2 || batch.UnknownIdentities[0] != "a" || batch.UnknownIdentities[1] != "z" {
		t.Fatalf("unknown = %v", batch.UnknownIdentities)
	}
}

func TestStatsProviderKey(t *testing.T) {
	provider := NewStatsProvider("mieru:server", nil)
	if provider.Key() != "mieru:server" {
		t.Fatalf("key = %q", provider.Key())
	}
}

func TestCappedBufferDropsOverflow(t *testing.T) {
	buf := &cappedBuffer{limit: 4}
	if n, err := buf.Write([]byte("abcdef")); err != nil || n != 6 {
		t.Fatalf("Write = %d %v", n, err)
	}
	if !buf.overflow || buf.String() != "abcd" {
		t.Fatalf("buf = %q overflow=%v", buf.String(), buf.overflow)
	}
}

func TestTailTruncates(t *testing.T) {
	if got := tail("hello", 10); got != "hello" {
		t.Fatalf("tail = %q", got)
	}
	if got := tail("0123456789", 4); got != "6789" {
		t.Fatalf("tail = %q", got)
	}
}
