package diagnostics

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

// TestExecHelperProcess is invoked as a fake external command by tests that
// replace execCommandContext. It is a no-op unless GO_WANT_EXEC_HELPER is set.
func TestExecHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_EXEC_HELPER") != "1" {
		return
	}
	defer os.Exit(0)

	switch os.Getenv("GO_EXEC_HELPER_CASE") {
	case "ping_success":
		fmt.Print(`PING 127.0.0.1 (127.0.0.1) 56(84) bytes of data.
64 bytes from 127.0.0.1: icmp_seq=1 ttl=64 time=1.2 ms

--- 127.0.0.1 ping statistics ---
5 packets transmitted, 3 received, 40% packet loss, time 3998ms
rtt min/avg/max/mdev = 1.2/3.4/5.6/0.1 ms`)
	case "ping_failure":
		fmt.Fprint(os.Stderr, "ping: unknown host")
		os.Exit(1)
	case "ping_failure_nooutput":
		os.Exit(1)
	case "ping_zero_transmitted":
		fmt.Print(`--- 127.0.0.1 ping statistics ---
0 packets transmitted, 0 received, 100% packet loss, time 0ms`)
	case "speedtest_cli_success":
		fmt.Print(`{"ping":11.2,"download":104000000,"upload":52000000,"server":{"sponsor":"Test ISP","name":"Moscow"}}`)
	case "speedtest_cli_failure":
		fmt.Fprint(os.Stderr, "speedtest-cli: command not found")
		os.Exit(1)
	case "ookla_success":
		fmt.Print(`{"ping":{"latency":9.5},"download":{"bandwidth":12500000},"upload":{"bandwidth":6250000},"server":{"name":"Moscow"},"isp":"Test ISP"}`)
	case "ookla_failure":
		fmt.Fprint(os.Stderr, "speedtest: command not found")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "unknown helper case %q\n", os.Getenv("GO_EXEC_HELPER_CASE"))
		os.Exit(2)
	}
}

// mockExecCommandContext replaces execCommandContext so that external commands
// are executed through TestExecHelperProcess with the requested case.
func mockExecCommandContext(t *testing.T, caseName string) {
	t.Helper()
	old := execCommandContext
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		cs := []string{"-test.run=TestExecHelperProcess", "--", name}
		cs = append(cs, arg...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_EXEC_HELPER=1", "GO_EXEC_HELPER_CASE="+caseName)
		return cmd
	}
	t.Cleanup(func() { execCommandContext = old })
}
