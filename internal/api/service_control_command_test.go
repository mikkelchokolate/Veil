package api

import "testing"

func TestServiceControlCommandMapsMieruToVeilManagedUnit(t *testing.T) {
	command, ok := NewServiceControlCommand().Build("mieru", "restart")
	if !ok {
		t.Fatal("mieru should be a known controlled service")
	}
	want := []string{"systemctl", "restart", "veil-mieru.service"}
	if len(command) != len(want) {
		t.Fatalf("command = %+v", command)
	}
	for i := range want {
		if command[i] != want[i] {
			t.Fatalf("command = %+v, want %+v", command, want)
		}
	}
}

func TestServiceControlCommandPreservesExistingServiceUnitsAndRejectsUnknown(t *testing.T) {
	module := NewServiceControlCommand()
	for _, tc := range []struct{ name, unit string }{
		{"veil", "veil.service"},
		{"caddy", "caddy.service"},
		{"hysteria2", "hysteria2.service"},
		{"sing-box", "sing-box.service"},
	} {
		command, ok := module.Build(tc.name, "restart")
		if !ok || command[2] != tc.unit {
			t.Fatalf("Build(%q) = %+v %v", tc.name, command, ok)
		}
	}
	if command, ok := module.Build("ssh", "restart"); ok || command != nil {
		t.Fatalf("unknown service command = %+v %v", command, ok)
	}
}
