//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRollbackWorkflow E2E tests the installer backup and CLI rollback restoration.
func TestRollbackWorkflow(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc")
	varDir := filepath.Join(dir, "var")
	backupDir := filepath.Join(varDir, "backups")

	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a dummy bin dir to mock systemctl and ufw commands
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		_ = os.WriteFile(filepath.Join(binDir, "systemctl.bat"), []byte("@exit /b 0"), 0o755)
		_ = os.WriteFile(filepath.Join(binDir, "ufw.bat"), []byte("@exit /b 0"), 0o755)
	} else {
		_ = os.WriteFile(filepath.Join(binDir, "systemctl"), []byte("#!/bin/sh\nexit 0"), 0o755)
		_ = os.WriteFile(filepath.Join(binDir, "ufw"), []byte("#!/bin/sh\nexit 0"), 0o755)
	}

	env := []string{"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")}

	// 1. Create a dummy file that the installer will overwrite/backup
	targetFile := filepath.Join(etcDir, "veil.env")
	originalContent := "VEIL_TEST_VARIABLE=original_value\n"
	if err := os.WriteFile(targetFile, []byte(originalContent), 0o600); err != nil {
		t.Fatalf("write initial file: %v", err)
	}

	systemdDir := filepath.Join(dir, "systemd")
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 2. Run the installer CLI
	installOut, err := runCLI(t, env,
		"install",
		"--panel-access", "local",
		"--domain", "panel.example.com",
		"--email", "admin@example.com",
		"--panel-port", "2096",
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
		"--yes",
	)
	if err != nil {
		t.Fatalf("install CLI failed: %v\nOutput: %s", err, installOut)
	}

	// 3. Verify that the file was overwritten by the installer
	overwrittenContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("read overwritten file: %v", err)
	}
	if string(overwrittenContent) == originalContent {
		t.Fatalf("expected file to be overwritten by install, but remained original")
	}

	// 4. Run `veil rollback list` to find the backup ID
	listOut, err := runCLI(t, nil, "rollback", "list", "--backup-dir", backupDir)
	if err != nil {
		t.Fatalf("rollback list failed: %v\nOutput: %s", err, listOut)
	}
	t.Logf("List output: %s", listOut)

	lines := strings.Split(listOut, "\n")
	var backupID string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.Contains(trimmed, "Backup ID") && !strings.Contains(trimmed, "---") {
			parts := strings.Fields(trimmed)
			if len(parts) > 0 {
				backupID = parts[0]
				break
			}
		}
	}
	if backupID == "" {
		t.Fatalf("no backup ID found in list output:\n%s", listOut)
	}

	// 5. Restore the backup via CLI
	restoreOut, err := runCLI(t, nil, "rollback", "restore", backupID, "--backup-dir", backupDir, "--yes")
	if err != nil {
		t.Fatalf("rollback restore failed: %v\nOutput: %s", err, restoreOut)
	}
	t.Logf("Restore output: %s", restoreOut)

	// 6. Verify that the original file content was successfully restored
	restoredContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(restoredContent) != originalContent {
		t.Fatalf("restore failed: expected %q, got %q", originalContent, string(restoredContent))
	}
}
