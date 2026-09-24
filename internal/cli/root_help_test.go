package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRootHelpDescribesPanelProtocolsWithoutTwoProtocolStackLanguage(t *testing.T) {
	help := executeHelp(t, "--help")
	if strings.Contains(help, "NaiveProxy + Hysteria2") {
		t.Fatalf("root help should not describe Veil as a two-protocol stack:\n%s", help)
	}
	if !strings.Contains(help, "protocols through Panel Inbounds") {
		t.Fatalf("root help should mention Panel Inbounds:\n%s", help)
	}
}

func TestHelpCommandPrintsOperatorCatalogOfNestedCommands(t *testing.T) {
	help := executeHelp(t, "help")
	for _, want := range []string{
		"protocols through Panel Inbounds",
		operatorHelpIntro,
		"veil help [command]",
		"veil admin",
		"veil admin reset",
		"veil admin set",
		"veil admin show",
		"veil admin rotate-key",
		"veil backup create",
		"veil backup list",
		"veil backup verify",
		"veil backup restore",
		"veil backup prune",
		"veil backup schedule",
		"veil backup schedule enable",
		"veil backup schedule disable",
		"veil config validate",
		"veil rollback list",
		"veil rollback restore",
		"veil rollback cleanup",
		"veil runtime install",
		"veil doctor",
		"veil install",
		"veil repair",
		"veil serve",
		"veil status",
		"veil update",
		"veil uninstall",
		"veil version",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("veil help missing %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "veil helper") {
		t.Fatalf("operator catalog should not list the hidden helper command:\n%s", help)
	}
}

func TestReadmeDocumentsOperatorHelpCatalog(t *testing.T) {
	body, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(body)
	for _, want := range []string{
		"docs/assets/veil-panel.gif",
		"veil help",
		"veil admin reset",
		"veil backup schedule enable",
		"veil runtime install",
		"veil rollback cleanup",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing %q", want)
		}
	}
	// #927: every row of the README Commands table must resolve to a real
	// registered subcommand — a documented-but-unwired command is a broken
	// contract, and a row deleted from the table while the command still
	// exists means the table silently lost coverage.
	root := NewRootCommand("veil")
	root.InitDefaultHelpCmd() // cobra registers `help` lazily at Execute time
	required := []string{
		"veil help",
		"veil install",
		"veil serve",
		"veil status",
		"veil doctor",
		"veil admin reset",
		"veil admin set",
		"veil admin show",
		"veil admin rotate-key",
		"veil config validate",
		"veil runtime install",
		"veil backup create",
		"veil backup list",
		"veil backup verify",
		"veil backup restore",
		"veil backup prune",
		"veil backup schedule enable",
		"veil backup schedule disable",
		"veil repair",
		"veil rollback list",
		"veil rollback restore",
		"veil rollback cleanup",
		"veil update",
		"veil uninstall",
		"veil version",
	}
	tableRows := map[string]bool{}
	for _, line := range strings.Split(readme, "\n") {
		fields := strings.Split(line, "|")
		if len(fields) < 3 {
			continue
		}
		cell := strings.TrimSpace(fields[1])
		cell = strings.Trim(cell, "`")
		if strings.HasPrefix(cell, "veil ") && !strings.ContainsAny(cell, "*[]") {
			tableRows[cell] = true
		}
	}
	for _, want := range required {
		if !tableRows[want] {
			t.Fatalf("README Commands table missing row %q", want)
		}
		// Resolve the documented command against the real command tree —
		// Find returns the deepest match; it must be a non-root command and
		// the flag-error path must not fire (unknown args return the root).
		args := strings.Fields(strings.TrimPrefix(want, "veil "))
		found, _, findErr := root.Find(args)
		if findErr != nil || found == root {
			t.Fatalf("README documents %q but no such command is registered", want)
		}
	}
}

func TestHelpCommandStillDocumentsSubcommandDetails(t *testing.T) {
	help := executeHelp(t, "help", "admin")
	for _, want := range []string{
		"Manage Veil admin accounts",
		"reset",
		"rotate-key",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("veil help admin missing %q:\n%s", want, help)
		}
	}
}

func executeHelp(t *testing.T, args ...string) string {
	t.Helper()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help %v: %v\n%s", args, err, out.String())
	}
	return out.String()
}
