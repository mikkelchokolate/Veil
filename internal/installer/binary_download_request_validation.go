package installer

import (
	"fmt"
	"strings"
)

type BinaryDownloadRequestValidation struct{}

func NewBinaryDownloadRequestValidation() BinaryDownloadRequestValidation {
	return BinaryDownloadRequestValidation{}
}

func (BinaryDownloadRequestValidation) Normalize(req DownloadRequest) (DownloadRequest, error) {
	if strings.TrimSpace(req.URL) == "" {
		return DownloadRequest{}, fmt.Errorf("download url is required")
	}
	if strings.TrimSpace(req.Destination) == "" {
		return DownloadRequest{}, fmt.Errorf("download destination is required")
	}
	if strings.TrimSpace(req.SHA256) == "" {
		return DownloadRequest{}, fmt.Errorf("sha256 checksum is required")
	}
	if req.Mode == 0 {
		req.Mode = 0o755
	}
	return req, nil
}
