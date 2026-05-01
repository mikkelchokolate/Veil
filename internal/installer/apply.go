package installer

import (
	"errors"
	"os"
	"path/filepath"
)

type ApplyPaths struct {
	EtcDir string
	VarDir string
}

type ApplyResult struct {
	CaddyfilePath     string
	Hysteria2Path     string
	FallbackIndexPath string
	WrittenFiles      []string
}

func ApplyRURecommendedProfile(profile RURecommendedProfile, paths ApplyPaths) (ApplyResult, error) {
	if paths.EtcDir == "" {
		return ApplyResult{}, errors.New("etc dir is required")
	}
	if paths.VarDir == "" {
		return ApplyResult{}, errors.New("var dir is required")
	}
	result := ApplyResult{
		CaddyfilePath:     filepath.Join(paths.EtcDir, "generated", "caddy", "Caddyfile"),
		Hysteria2Path:     filepath.Join(paths.EtcDir, "generated", "hysteria2", "server.yaml"),
		FallbackIndexPath: filepath.Join(paths.VarDir, "www", "index.html"),
	}
	if err := writeManagedFile(result.CaddyfilePath, profile.Caddyfile, 0o600); err != nil {
		return ApplyResult{}, err
	}
	result.WrittenFiles = append(result.WrittenFiles, result.CaddyfilePath)
	if err := writeManagedFile(result.Hysteria2Path, profile.Hysteria2YAML, 0o600); err != nil {
		return ApplyResult{}, err
	}
	result.WrittenFiles = append(result.WrittenFiles, result.Hysteria2Path)
	if err := writeManagedFile(result.FallbackIndexPath, fallbackIndexHTML(profile.Domain), 0o644); err != nil {
		return ApplyResult{}, err
	}
	result.WrittenFiles = append(result.WrittenFiles, result.FallbackIndexPath)
	return result, nil
}

func writeManagedFile(path string, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fallbackIndexHTML(domain string) string {
	if domain == "" {
		domain = "Veil"
	}
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + domain + `</title>
</head>
<body>
  <h1>Veil</h1>
  <p>This site is served by Veil.</p>
</body>
</html>
`
}
