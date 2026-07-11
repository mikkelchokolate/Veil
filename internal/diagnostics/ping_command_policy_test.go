package diagnostics

import (
	"reflect"
	"testing"
	"time"
)

func TestPingCommandPolicyBuildsArgsAndTimeout(t *testing.T) {
	policy := NewPingCommandPolicy()
	if got, want := policy.Timeout(3), 5*time.Second; got != want {
		t.Fatalf("timeout = %v, want %v", got, want)
	}
	args := policy.Args("example.com", 3)
	want := []string{"-c", "3", "-W", "2", "--", "example.com"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %+v, want %+v", args, want)
	}
}

func TestPingCommandPolicyKeepsOptionLikeHostAsOperand(t *testing.T) {
	args := NewPingCommandPolicy().Args("-f", 1)
	want := []string{"-c", "1", "-W", "2", "--", "-f"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %+v, want %+v", args, want)
	}
}
