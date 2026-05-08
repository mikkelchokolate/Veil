package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"
)

type LegacyCLICompatibility struct {
	stack  string
	domain string
	email  string
	port   int
}

func NewLegacyCLICompatibility() *LegacyCLICompatibility {
	return &LegacyCLICompatibility{}
}

func (c *LegacyCLICompatibility) RegisterInstallFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.stack, "stack", "", "deprecated; Veil install only installs Panel, protocols are configured as Panel Inbounds")
	cmd.Flags().IntVar(&c.port, "port", 0, "deprecated; protocols are configured as Panel Inbounds")
	c.hide(cmd, "stack", "port")
}

func (c *LegacyCLICompatibility) RegisterRepairFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.stack, "stack", "panel", "deprecated; repair uses Panel install and Panel state")
	cmd.Flags().StringVar(&c.domain, "domain", "", "deprecated; protocols are configured as Panel Inbounds")
	cmd.Flags().StringVar(&c.email, "email", "", "deprecated; protocols are configured as Panel Inbounds")
	cmd.Flags().IntVar(&c.port, "port", 0, "deprecated; protocol ports come from Panel Inbounds")
	c.hide(cmd, "stack", "domain", "email", "port")
}

func (c *LegacyCLICompatibility) RejectStackSelection(message string) error {
	stack := strings.TrimSpace(c.stack)
	if stack == "" || stack == "panel" {
		return nil
	}
	return errors.New(message)
}

func (c *LegacyCLICompatibility) hide(cmd *cobra.Command, names ...string) {
	for _, name := range names {
		_ = cmd.Flags().MarkHidden(name)
	}
}
