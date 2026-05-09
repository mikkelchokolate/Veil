package installer

import (
	"os"

	"github.com/veil-panel/veil/internal/managedfiles"
)

type ApplyPaths struct {
	EtcDir      string
	VarDir      string
	SystemdDir  string
	BackupDir   string
	VeilBinary  string
	CaddyBinary string
}

type ApplyResult struct {
	CaddyfilePath     string
	Hysteria2Path     string
	FallbackIndexPath string
	WrittenFiles      []string
	BackupID          string
}

func writeManagedFile(path string, content string, mode os.FileMode) error {
	return managedfiles.WriteFile(path, content, mode)
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
