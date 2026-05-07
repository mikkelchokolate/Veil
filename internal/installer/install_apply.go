package installer

import "path/filepath"

type InstallApply struct {
	profile RURecommendedProfile
	paths   ApplyPaths
}

func NewInstallApply(profile RURecommendedProfile, paths ApplyPaths) InstallApply {
	return InstallApply{profile: profile, paths: paths}
}

func (a InstallApply) Apply() (ApplyResult, error) {
	files, err := NewManagedFileRepair(a.profile, a.paths).desiredFiles()
	if err != nil {
		return ApplyResult{}, err
	}
	result := ApplyResult{
		CaddyfilePath:     filepath.Join(a.paths.EtcDir, "generated", "caddy", "Caddyfile"),
		Hysteria2Path:     filepath.Join(a.paths.EtcDir, "generated", "hysteria2", "server.yaml"),
		FallbackIndexPath: filepath.Join(a.paths.VarDir, "www", "index.html"),
	}
	if a.paths.BackupDir != "" {
		existingPaths := make([]string, 0, len(files))
		for _, file := range files {
			existingPaths = append(existingPaths, file.Path)
		}
		backupID, err := NewBackupLifecycle(a.paths.BackupDir).BackupExisting(existingPaths)
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

func ApplyRURecommendedProfile(profile RURecommendedProfile, paths ApplyPaths) (ApplyResult, error) {
	return NewInstallApply(profile, paths).Apply()
}
