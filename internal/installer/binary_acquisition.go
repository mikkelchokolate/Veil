package installer

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type BinaryAcquisitionModule struct {
	binary BinaryAcquisition
}

func NewBinaryAcquisitionModule(binary BinaryAcquisition) BinaryAcquisitionModule {
	return BinaryAcquisitionModule{binary: binary}
}

func (m BinaryAcquisitionModule) RepairPlan() (BinaryRepairPlan, error) {
	binary := m.binary
	if strings.TrimSpace(binary.Name) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("binary name is required")
	}
	if strings.TrimSpace(binary.URL) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("binary url is required")
	}
	if strings.TrimSpace(binary.Destination) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("binary destination is required")
	}
	if strings.TrimSpace(binary.SHA256) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("sha256 checksum is required for binary repair")
	}
	body, err := os.ReadFile(binary.Destination)
	if os.IsNotExist(err) {
		return BinaryRepairPlan{Actions: []BinaryRepairAction{{Name: binary.Name, URL: binary.URL, Destination: binary.Destination, SHA256: binary.SHA256, Reason: RepairReasonMissing}}}, nil
	}
	if err != nil {
		return BinaryRepairPlan{}, err
	}
	actual, err := SHA256Hex(body)
	if err != nil {
		return BinaryRepairPlan{}, err
	}
	if actual != strings.ToLower(strings.TrimSpace(binary.SHA256)) {
		return BinaryRepairPlan{Actions: []BinaryRepairAction{{Name: binary.Name, URL: binary.URL, Destination: binary.Destination, SHA256: binary.SHA256, Reason: RepairReasonDrifted}}}, nil
	}
	return BinaryRepairPlan{}, nil
}

func (m BinaryAcquisitionModule) DownloadVerified(ctx context.Context, client *http.Client) (DownloadResult, error) {
	binary := m.binary
	return DownloadVerifiedBinary(ctx, client, DownloadRequest{
		URL:         binary.URL,
		Destination: binary.Destination,
		SHA256:      binary.SHA256,
		Mode:        0o755,
	})
}
