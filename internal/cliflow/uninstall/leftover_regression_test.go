package uninstall

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestUninstallRemovesBackupDropInTemplateWantsAndRuntimeState(t *testing.T) {
	host := t.TempDir()
	systemdDir := filepath.Join(host, "systemd")
	wantsDir := filepath.Join(systemdDir, "multi-user.target.wants")
	dropInDir := filepath.Join(systemdDir, "veil-backup.service.d")
	caddyDir := filepath.Join(host, "var", "lib", "caddy")
	mitaDir := filepath.Join(host, "var", "lib", "mita")
	hy2Want := filepath.Join(wantsDir, "veil-hysteria2@hy2-main.service")
	olcWant := filepath.Join(wantsDir, "veil-olcrtc@room-1.service")
	for _, dir := range []string{wantsDir, dropInDir, filepath.Join(caddyDir, "caddy", "certificates"), mitaDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dropInDir, "passphrase-path.conf"), []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hy2Want, []byte("symlink"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(olcWant, []byte("symlink"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caddyDir, "caddy", "certificates", "example.key"), []byte("acme-key"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stopped, removed []string
	var out, errOut bytes.Buffer
	opts := Options{
		Yes: true, EtcDir: filepath.Join(host, "etc", "veil"), VarDir: filepath.Join(host, "var", "lib", "veil"),
		SystemdDir: systemdDir, InstallDir: filepath.Join(host, "bin"),
		CaddyStateDir: caddyDir, MitaStateDir: mitaDir,
	}
	err := Run(opts, &out, &errOut, Dependencies{
		ServiceStopper:  func(service string) error { stopped = append(stopped, service); return nil },
		FileRemover:     func(path string) error { removed = append(removed, filepath.ToSlash(path)); return nil },
		SystemdReloader: func() error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"veil-hysteria2@hy2-main.service", "veil-olcrtc@room-1.service"} {
		if !contains(stopped, want) {
			t.Fatalf("did not disable leftover instance %s, stopped=%v", want, stopped)
		}
	}
	for _, want := range []string{
		filepath.ToSlash(dropInDir),
		filepath.ToSlash(hy2Want),
		filepath.ToSlash(olcWant),
		filepath.ToSlash(caddyDir),
		filepath.ToSlash(mitaDir),
	} {
		if !contains(removed, want) {
			t.Fatalf("did not remove %s, removed=%v", want, removed)
		}
	}
}

// Issue #642: uninstall used to delete only veil-backup.service.d, leaving
// 10-veil-install.conf drop-ins for veil.service / veil-helper.service /
// veil-caddy.service / protocol units behind — where the next package
// install would silently merge the stale custom-path overrides.
func TestUninstallRemovesEveryManagedUnitDropInDir(t *testing.T) {
	host := t.TempDir()
	systemdDir := filepath.Join(host, "systemd")
	vendorDir := filepath.Join(host, "vendor")
	var dropInDirs []string
	for _, dir := range []string{systemdDir, vendorDir} {
		for _, unit := range Services() {
			dropIn := filepath.Join(dir, unit+".d")
			if err := os.MkdirAll(dropIn, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dropIn, "10-veil-install.conf"), []byte("[Service]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			dropInDirs = append(dropInDirs, filepath.ToSlash(dropIn))
		}
	}
	if len(dropInDirs) == 0 {
		t.Fatal("expected at least one managed unit drop-in dir")
	}

	var removed []string
	err := Run(Options{
		Yes:    true,
		EtcDir: filepath.Join(host, "etc", "veil"), VarDir: filepath.Join(host, "var", "lib", "veil"),
		SystemdDir: systemdDir, InstallDir: filepath.Join(host, "bin"),
		CaddyStateDir:     filepath.Join(host, "var", "lib", "caddy"),
		MitaStateDir:      filepath.Join(host, "var", "lib", "mita"),
		VendorSystemdDirs: []string{vendorDir},
	}, new(bytes.Buffer), new(bytes.Buffer), Dependencies{
		ServiceStopper:  func(string) error { return nil },
		FileRemover:     func(path string) error { removed = append(removed, filepath.ToSlash(path)); return nil },
		SystemdReloader: func() error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range dropInDirs {
		if !contains(removed, want) {
			t.Fatalf("uninstall left drop-in dir %s behind, removed=%v", want, removed)
		}
	}
}

func TestUninstallKeepDataStillRemovesSystemdDropInAndInstanceWants(t *testing.T) {
	host := t.TempDir()
	systemdDir := filepath.Join(host, "systemd")
	wantsDir := filepath.Join(systemdDir, "multi-user.target.wants")
	dropInDir := filepath.Join(systemdDir, "veil-backup.service.d")
	caddyDir := filepath.Join(host, "var", "lib", "caddy")
	mitaDir := filepath.Join(host, "var", "lib", "mita")
	hy2Want := filepath.Join(wantsDir, "veil-hysteria2@hy2-main.service")
	if err := os.MkdirAll(wantsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dropInDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hy2Want, []byte("symlink"), 0o644); err != nil {
		t.Fatal(err)
	}
	var removed []string
	err := Run(Options{
		Yes: true, KeepData: true,
		EtcDir: filepath.Join(host, "etc", "veil"), VarDir: filepath.Join(host, "var", "lib", "veil"),
		SystemdDir: systemdDir, InstallDir: filepath.Join(host, "bin"),
		CaddyStateDir: caddyDir, MitaStateDir: mitaDir,
	}, new(bytes.Buffer), new(bytes.Buffer), Dependencies{
		ServiceStopper:  func(string) error { return nil },
		FileRemover:     func(path string) error { removed = append(removed, filepath.ToSlash(path)); return nil },
		SystemdReloader: func() error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{
		filepath.ToSlash(filepath.Join(host, "etc", "veil")),
		filepath.ToSlash(filepath.Join(host, "var", "lib", "veil")),
		filepath.ToSlash(caddyDir),
		filepath.ToSlash(mitaDir),
	} {
		if contains(removed, preserved) {
			t.Fatalf("--keep-data removed preserved path %s: %v", preserved, removed)
		}
	}
	for _, want := range []string{filepath.ToSlash(dropInDir), filepath.ToSlash(hy2Want)} {
		if !contains(removed, want) {
			t.Fatalf("--keep-data must still remove systemd leftover %s: %v", want, removed)
		}
	}
}
