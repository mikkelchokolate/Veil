package installer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/managedfiles"
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
	CaddyJSONPath     string
	Hysteria2Path     string
	FallbackIndexPath string
	WrittenFiles      []string
	BackupID          string
}

// newUFWApplier is overridable in tests so that firewall success and error
// paths can be exercised without depending on a real ufw installation.
var newUFWApplier = firewall.NewUFWApplier

type InstallApply struct {
	profile         RURecommendedProfile
	paths           ApplyPaths
	firewallActions []firewall.Rule
}

func NewInstallApply(profile RURecommendedProfile, paths ApplyPaths) InstallApply {
	return InstallApply{profile: profile, paths: paths}
}

// NewInstallApplyWithPlan creates an installer that also applies firewall rules.
func NewInstallApplyWithPlan(profile RURecommendedProfile, paths ApplyPaths, plan InstallPlan) InstallApply {
	return InstallApply{profile: profile, paths: paths, firewallActions: plan.FirewallActions}
}

func (a InstallApply) Apply() (ApplyResult, error) {
	files, err := desiredManagedFiles(a.profile, a.paths)
	if err != nil {
		return ApplyResult{}, err
	}
	result := ApplyResult{
		CaddyJSONPath:     filepath.Join(a.paths.EtcDir, "generated", "caddy", "config.json"),
		Hysteria2Path:     filepath.Join(a.paths.EtcDir, "generated", "hysteria2", "server.yaml"),
		FallbackIndexPath: filepath.Join(a.paths.VarDir, "www", "index.html"),
	}
	if a.paths.BackupDir != "" {
		existingPaths := make([]string, 0, len(files))
		for _, file := range files {
			existingPaths = append(existingPaths, file.Path)
		}
		backupID, err := backup.NewLifecycle(a.paths.BackupDir).BackupExisting(existingPaths)
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

	if len(a.firewallActions) > 0 {
		applier := newUFWApplier()
		if err := applier.EnsureActive(); err != nil {
			return result, fmt.Errorf("enable firewall: %w", err)
		}
		if err := applier.ApplyRules(a.firewallActions); err != nil {
			return result, fmt.Errorf("apply firewall rules: %w", err)
		}
	}

	return result, nil
}

func ApplyRURecommendedProfile(profile RURecommendedProfile, paths ApplyPaths) (ApplyResult, error) {
	return NewInstallApply(profile, paths).Apply()
}

// ApplyRURecommendedProfileWithPlan applies the install profile and executes the firewall rules from the install plan.
func ApplyRURecommendedProfileWithPlan(profile RURecommendedProfile, paths ApplyPaths, plan InstallPlan) (ApplyResult, error) {
	return NewInstallApplyWithPlan(profile, paths, plan).Apply()
}

func writeManagedFile(path string, content string, mode os.FileMode) error {
	return managedfiles.WriteFile(path, content, mode)
}
