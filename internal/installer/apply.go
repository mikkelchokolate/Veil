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

func ApplyRURecommendedProfile(profile RURecommendedProfile, paths ApplyPaths) (ApplyResult, error) {
	files, err := desiredManagedFiles(profile, paths)
	if err != nil {
		return ApplyResult{}, err
	}
	result := ApplyResult{
		CaddyfilePath:     filepath.Join(paths.EtcDir, "generated", "caddy", "Caddyfile"),
		Hysteria2Path:     filepath.Join(paths.EtcDir, "generated", "hysteria2", "server.yaml"),
		FallbackIndexPath: filepath.Join(paths.VarDir, "www", "index.html"),
	}

	// Backup existing files before overwriting
	if paths.BackupDir != "" {
		existingPaths := make([]string, 0, len(files))
		for _, file := range files {
			existingPaths = append(existingPaths, file.Path)
		}
		backupID, err := BackupBeforeApply(existingPaths, paths.BackupDir)
		if err != nil {
			return ApplyResult{}, err
		}
		result.BackupID = backupID
	}

	for _, file := range files {
		if err := writeManagedFile(file.Path, file.Content, file.Mode); err != nil {
			return ApplyResult{}, err
		}
		result.WrittenFiles = append(result.WrittenFiles, file.Path)
	}
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
