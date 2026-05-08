package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

type BuildHint struct {
	BinaryPath string
	Commands   []string
}

const maxReleaseAssetSize = 100 * 1024 * 1024 // 100 MB

type DownloadRequest struct {
	URL         string
	Destination string
	SHA256      string
	Mode        os.FileMode
}

type DownloadResult struct {
	URL         string
	Destination string
	SHA256      string
	Bytes       int64
}

func SHA256Hex(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func VerifySHA256Hex(data []byte, expected string) error {
	actual, err := SHA256Hex(data)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func CaddyPanelBuildHint(binaryPath string) BuildHint {
	if binaryPath == "" {
		binaryPath = "/usr/local/bin/caddy"
	}
	return BuildHint{BinaryPath: binaryPath, Commands: []string{"requires standard Caddy at " + binaryPath}}
}
