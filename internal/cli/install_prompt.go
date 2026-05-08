package cli

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type InstallPrompt struct {
	in  io.Reader
	out io.Writer
}

func NewInstallPrompt(in io.Reader, out io.Writer) InstallPrompt {
	return InstallPrompt{in: in, out: out}
}

func (p InstallPrompt) PromptMissingOptions(panelAccess string, domain *string, email *string, panelPort *int) error {
	reader := bufio.NewReader(p.in)
	domainPattern := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)
	if panelAccess == "caddy" {
		if strings.TrimSpace(*domain) == "" {
			for {
				fmt.Fprint(p.out, "Domain for Veil/ACME: ")
				value, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				candidate := strings.TrimSpace(value)
				if candidate == "" {
					fmt.Fprintln(p.out, "Domain must not be empty.")
					continue
				}
				if !domainPattern.MatchString(candidate) {
					fmt.Fprintln(p.out, "Domain must be a valid domain name (e.g. example.com).")
					continue
				}
				*domain = candidate
				break
			}
		}
		if strings.TrimSpace(*email) == "" {
			fmt.Fprint(p.out, "ACME email: ")
			value, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			*email = strings.TrimSpace(value)
		}
	}
	if *panelPort == 0 {
		fmt.Fprint(p.out, "Customize panel port? If no, Veil will choose a random high port. [y/N]: ")
		value, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		answer := strings.ToLower(strings.TrimSpace(value))
		if answer == "y" || answer == "yes" {
			for {
				fmt.Fprint(p.out, "Panel TCP port: ")
				value, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				parsed, err := strconv.Atoi(strings.TrimSpace(value))
				if err != nil {
					fmt.Fprintln(p.out, "Port must be a number between 1 and 65535.")
					continue
				}
				if parsed < 1 || parsed > 65535 {
					fmt.Fprintln(p.out, "Port must be between 1 and 65535.")
					continue
				}
				*panelPort = parsed
				break
			}
		}
	}
	return nil
}

func promptInstallOptions(cmd *cobra.Command, panelAccess string, domain *string, email *string, panelPort *int) error {
	return NewInstallPrompt(cmd.InOrStdin(), cmd.OutOrStdout()).PromptMissingOptions(panelAccess, domain, email, panelPort)
}
