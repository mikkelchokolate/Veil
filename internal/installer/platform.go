package installer

import (
	"fmt"
	"runtime"
)

type Platform struct {
	OS   string
	Arch string
}

func CurrentPlatform() Platform {
	return Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
}

func NormalizeArch(arch string) (string, error) {
	switch arch {
	case "amd64", "x86_64":
		return "amd64", nil
	case "arm64", "aarch64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported arch %q", arch)
	}
}

func ValidateLinuxPlatform(platform Platform) error {
	if platform.OS != "linux" {
		return fmt.Errorf("unsupported os %q; linux is required", platform.OS)
	}
	_, err := NormalizeArch(platform.Arch)
	return err
}
