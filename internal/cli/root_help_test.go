package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpDescribesPanelProtocolsWithoutTwoProtocolStackLanguage(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	help := out.String()
	if strings.Contains(help, "NaiveProxy + Hysteria2") {
		t.Fatalf("root help should not describe Veil as a two-protocol stack:\n%s", help)
	}
	if !strings.Contains(help, "protocols through Panel Inbounds") {
		t.Fatalf("root help should mention Panel Inbounds:\n%s", help)
	}
}
