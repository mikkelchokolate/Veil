package protocols

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

func fakeRuntimeChecksum(name string, body []byte) []byte {
	digest := sha256.Sum256(body)
	return []byte(hex.EncodeToString(digest[:]) + "  " + name + "\n")
}

func fakeProtocolRuntimeVersion(_ context.Context, path string, _ []string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	switch {
	case strings.Contains(string(body), "demo"):
		return "demo v1", nil
	case strings.Contains(string(body), "hysteria"):
		return "hysteria 2.10.0", nil
	case strings.Contains(string(body), "mita"):
		return "mita 3.34.1", nil
	case strings.Contains(string(body), "sing-box"):
		return "sing-box 1.13.14", nil
	case strings.Contains(string(body), "caddy"):
		return "caddy 2.11.4", nil
	default:
		return "", fmt.Errorf("unknown fake runtime marker in %s", path)
	}
}

func fakeOlcrtcBuildInfo(string) (string, error) {
	return "github.com/openlibrecommunity/olcrtc@v0.0.0-20260814163019-48cae636f88e", nil
}
