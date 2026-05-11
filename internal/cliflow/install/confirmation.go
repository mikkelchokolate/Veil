package install

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func ConfirmPlan(in io.Reader, out io.Writer, interactive bool) error {
	if interactive {
		fmt.Fprint(out, "Apply install plan? [y/N]: ")
		answer, err := bufio.NewReader(in).ReadString('\n')
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
