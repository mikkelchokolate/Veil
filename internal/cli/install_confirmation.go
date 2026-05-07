package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func confirmInstallPlan(cmd *cobra.Command, interactive bool) error {
	if interactive {
		fmt.Fprint(cmd.OutOrStdout(), "Apply install plan? [y/N]: ")
		answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil {
			return fmt.Errorf("read confirmation: %w", err)
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			return fmt.Errorf("install cancelled")
		}
		return nil
	}
	return fmt.Errorf("apply mode requires --yes; rerun with --dry-run to preview")
}
