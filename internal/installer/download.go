package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

type BuildHint struct {
	BinaryPath string
	Commands   []string
}

func Hysteria2ReleaseAssetURL(version, goos, arch string) (string, error) {
	if version == "" {
		return "", fmt.Errorf("version is required")
	}
	if goos != "linux" {
		return "", fmt.Errorf("unsupported os %q", goos)
	}
	mappedArch, err := hysteriaArch(arch)
	if err != nil {
		return "", err
	}
	tag := "app/" + version
	asset := fmt.Sprintf("hysteria-%s-%s", goos, mappedArch)
	return fmt.Sprintf("https://github.com/apernet/hysteria/releases/download/%s/%s", url.PathEscape(tag), asset), nil
}

func hysteriaArch(arch string) (string, error) {
	switch arch {
	case "amd64", "x86_64":
		return "amd64", nil
	case "arm64", "aarch64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported arch %q", arch)
	}
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

func CaddyNaiveBuildHint(binaryPath string) BuildHint {
	if binaryPath == "" {
		binaryPath = "/usr/local/bin/caddy"
	}
	return BuildHint{
		BinaryPath: binaryPath,
		Commands: []string{
			"go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest",
			"xcaddy build --with github.com/caddyserver/forwardproxy=github.com/klzgrad/forwardproxy@naive",
			"install -m 0755 ./caddy " + binaryPath,
		},
	}
}
