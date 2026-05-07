package installer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryAcquisitionModulePlansAndDownloadsVerifiedBinary(t *testing.T) {
	body := []byte("binary-body")
	checksum, err := SHA256Hex(body)
	if err != nil {
		t.Fatalf("SHA256Hex: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	dest := filepath.Join(t.TempDir(), "hysteria")
	module := NewBinaryAcquisitionModule(BinaryAcquisition{Name: "hysteria2", URL: server.URL, Destination: dest, SHA256: checksum})

	plan, err := module.RepairPlan()
	if err != nil {
		t.Fatalf("RepairPlan: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Reason != RepairReasonMissing {
		t.Fatalf("plan = %+v", plan)
	}
	result, err := module.DownloadVerified(context.Background(), server.Client())
	if err != nil {
		t.Fatalf("DownloadVerified: %v", err)
	}
	if result.Destination != dest || result.SHA256 != checksum {
		t.Fatalf("result = %+v", result)
	}
	stored, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(body) {
		t.Fatalf("stored = %q", string(stored))
	}
}
