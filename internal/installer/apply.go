package installer

import (
	"os"
	"path/filepath"
)

type ApplyPaths struct {
	EtcDir     string
	VarDir     string
	SystemdDir string
	BackupDir  string
}

type ApplyResult struct {
	CaddyfilePath     string
	Hysteria2Path     string
	FallbackIndexPath string
	WrittenFiles      []string
	BackupID          string
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
