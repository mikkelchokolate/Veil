package service

import (
	"reflect"
	"testing"
	"time"
)

func TestSystemdServiceStatusCommandBuildsCommandArgsAndTimeout(t *testing.T) {
	command := NewSystemdServiceStatusCommand("caddy.service")
	if command.Name() != "systemctl" {
		t.Fatalf("name = %q", command.Name())
	}
	wantArgs := []string{"show", "caddy.service", "--property=LoadState", "--property=ActiveState", "--property=SubState", "--property=MainPID", "--property=ExecMainStartTimestampMonotonic", "--no-page"}
	if !reflect.DeepEqual(command.Args(), wantArgs) {
		t.Fatalf("args = %+v", command.Args())
	}
	if command.Timeout() != 5*time.Second {
		t.Fatalf("timeout = %v", command.Timeout())
	}
}
