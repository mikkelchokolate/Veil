package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPlainVeilUsesMenuOnlyOnInteractiveTerminal(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		oldTerminal := operatorMenuTerminal
		operatorMenuTerminal = func(*cobra.Command) bool { return terminal }
		cmd := NewRootCommand("test")
		var out bytes.Buffer
		cmd.SetIn(strings.NewReader("0\n"))
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		operatorMenuTerminal = oldTerminal
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "Choose an action:") != terminal {
			t.Fatalf("terminal=%v, output:\n%s", terminal, out.String())
		}
		if !terminal && !strings.Contains(out.String(), "Usage:") {
			t.Fatalf("nonterminal invocation must retain help:\n%s", out.String())
		}
	}
}

func TestExplicitMenuAcceptsEOFAndInvalidInput(t *testing.T) {
	for _, input := range []string{"", "invalid\n0\n", "0"} {
		cmd := NewRootCommand("test")
		var out bytes.Buffer
		cmd.SetIn(strings.NewReader(input))
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"menu"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Choose an action:") {
			t.Fatalf("explicit menu did not open:\n%s", out.String())
		}
		if strings.HasPrefix(input, "invalid") && !strings.Contains(out.String(), "unknown choice") {
			t.Fatalf("invalid choice was not explained:\n%s", out.String())
		}
	}
}

func TestMenuActionsStayInMenuAfterFailureAndKeepRuntimeSelection(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("6\n14\nmieru\n14\nunknown\n0\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	var calls [][]string
	err := runOperatorMenu(cmd, "test", RootOptions{}, func(args []string) error {
		calls = append(calls, args)
		if args[0] == "doctor" {
			return errors.New("diagnostic failure")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"doctor"}, {"runtime", "install", "--only", "mieru"}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("commands = %v, want %v", calls, want)
	}
	for _, message := range []string{"diagnostic failure", "unknown runtime name"} {
		if !strings.Contains(out.String(), message) {
			t.Fatalf("menu did not explain %q:\n%s", message, out.String())
		}
	}
}

func TestMenuRejectsDelimiterOnlyRuntimeSelection(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("14\n , , \n0\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := runOperatorMenu(cmd, "test", RootOptions{}, func([]string) error {
		t.Fatal("delimiter-only selection must not install all runtimes")
		return nil
	})
	if err != nil || !strings.Contains(out.String(), "at least one runtime") {
		t.Fatalf("invalid runtime selection: %v\n%s", err, out.String())
	}
}

func TestMenuMutationRequiresConfirmationAfterPreview(t *testing.T) {
	for _, answer := range []string{"yes", "no", ""} {
		cmd := NewRootCommand("test")
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		var calls [][]string
		err := operatorMenuAction(cmd, bufio.NewReader(strings.NewReader(answer+"\n")), "13", func(args []string) error {
			calls = append(calls, args)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		want := [][]string{{"update", "--dry-run"}}
		if answer == "yes" {
			want = append(want, []string{"update", "--yes"})
		}
		if !reflect.DeepEqual(calls, want) {
			t.Fatalf("answer=%q commands=%v, want %v", answer, calls, want)
		}
	}
}

func TestMenuInstallRetainsInteractivePromptsAndConfirmation(t *testing.T) {
	cmd := NewRootCommand("test")
	var args []string
	err := operatorMenuAction(cmd, bufio.NewReader(strings.NewReader("")), "16", func(value []string) error {
		args = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) < 2 || args[0] != "install" || args[1] != "--interactive" || strings.Contains(strings.Join(args, " "), "--yes") {
		t.Fatalf("menu install skipped its interactive confirmation: %v", args)
	}
}

func TestMenuServiceRecoveryDoesNotUsePanelAndRejectsOtherUnits(t *testing.T) {
	oldRun := operatorSystemCommand
	defer func() { operatorSystemCommand = oldRun }()
	var calls [][]string
	operatorSystemCommand = func(_ *cobra.Command, name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}
	cmd := NewRootCommand("test")
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	noPanel := func([]string) error { t.Fatal("service recovery dispatched a Veil API command"); return nil }
	if err := operatorMenuAction(cmd, bufio.NewReader(strings.NewReader("veil-helper.service\nyes\n")), "4", noPanel); err != nil {
		t.Fatal(err)
	}
	if err := operatorMenuAction(cmd, bufio.NewReader(strings.NewReader("ssh.service\nyes\n")), "3", noPanel); err == nil {
		t.Fatal("menu accepted an unmanaged service")
	}
	if want := [][]string{{"systemctl", "restart", "veil-helper.service"}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("service commands=%v, want %v", calls, want)
	}
}

func TestMenuPrintsDirectPanelAddressFromInstalledFiles(t *testing.T) {
	etcDir := t.TempDir()
	body := "VEIL_PANEL_ACCESS=direct\nVEIL_LISTEN=0.0.0.0:2096\nVEIL_DOMAIN=203.0.113.4\nVEIL_WEB_BASE_PATH=/secret/\nVEIL_TLS_CERT=/etc/veil/panel/tls.crt\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"VEIL_PANEL_ACCESS", "VEIL_LISTEN", "VEIL_DOMAIN", "VEIL_WEB_BASE_PATH", "VEIL_TLS_CERT"} {
		t.Setenv(key, "")
	}
	var out bytes.Buffer
	printOperatorPanelAddress(&out, etcDir)
	if !strings.Contains(out.String(), "https://203.0.113.4:2096/secret/") {
		t.Fatalf("incorrect direct URL:\n%s", out.String())
	}
}
