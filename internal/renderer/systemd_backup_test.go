package renderer

import (
	"strings"
	"testing"
)

func TestRenderSystemdUnitsIncludesHardenedEncryptedBackupTimer(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{VeilBinary: "/opt/veil/bin/veil"})
	service := units[UnitBackupService]
	timer := units[UnitBackupTimer]
	for _, want := range []string{
		"Type=oneshot",
		"/opt/veil/bin/veil backup create",
		"--passphrase-file /etc/veil/backup.passphrase",
		"--output-dir /var/lib/veil/backups",
		"--prune",
		"NoNewPrivileges=true",
		"ProtectSystem=strict",
		"ReadWritePaths=/var/lib/veil/backups",
	} {
		if !strings.Contains(service, want) {
			t.Fatalf("backup service missing %q:\n%s", want, service)
		}
	}
	for _, want := range []string{
		"OnCalendar=*-*-* 02:00:00",
		"RandomizedDelaySec=30m",
		"Persistent=true",
		"WantedBy=timers.target",
	} {
		if !strings.Contains(timer, want) {
			t.Fatalf("backup timer missing %q:\n%s", want, timer)
		}
	}
}

func TestManagedSystemdUnitNamesIncludesBackupUnits(t *testing.T) {
	names := ManagedSystemdUnitNames()
	if !containsString(names, UnitBackupService) || !containsString(names, UnitBackupTimer) {
		t.Fatalf("managed unit names=%v", names)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
