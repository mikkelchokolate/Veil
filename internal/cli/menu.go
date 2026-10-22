package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	installflow "github.com/mikkelchokolate/Veil/internal/cliflow/install"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/syspath"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var operatorMenuTerminal = func(cmd *cobra.Command) bool {
	in, inputFile := cmd.InOrStdin().(*os.File)
	out, outputFile := cmd.OutOrStdout().(*os.File)
	return inputFile && outputFile && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}

var operatorSystemCommand = func(cmd *cobra.Command, name string, args ...string) error {
	process := syspath.CommandContext(cmd.Context(), name, args...)
	process.Stdin, process.Stdout, process.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
	return process.Run()
}

const operatorMenuText = `
Veil management and recovery
 1  Service status and panel address
 2  Start a service
 3  Stop a service
 4  Restart a service
 5  Recent service logs
 6  Host diagnostics
 7  Repair installed configuration
 8  Reset administrator credentials
 9  Create an encrypted backup
10  List backups
11  Restore an encrypted backup
12  Roll back configuration files
13  Update Veil
14  Install selected protocol runtimes
15  Uninstall Veil (keep configuration and credentials)
16  Install or reconfigure the panel
 0  Exit
`

func newMenuCommand(version string, options RootOptions) *cobra.Command {
	return &cobra.Command{
		Use: "menu", Short: "Open the terminal management and recovery menu",
		Long: "Manage local Veil services, logs, credentials, backups, updates and runtimes.\nThe menu works even when the web panel or privileged helper is stopped.\nHost changes require root; use sudo veil. Without a terminal, use veil menu\nexplicitly or keep using individual commands for scripts.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOperatorMenu(cmd, version, options, nil)
		},
	}
}

func runOperatorMenu(cmd *cobra.Command, version string, options RootOptions, execute func([]string) error) error {
	reader := bufio.NewReader(cmd.InOrStdin())
	if execute == nil {
		execute = func(args []string) error {
			// Each invocation gets fresh flags; a previous --yes must not leak.
			child := NewRootCommandWithOptions(version, options)
			child.SetIn(reader)
			child.SetOut(cmd.OutOrStdout())
			child.SetErr(cmd.ErrOrStderr())
			child.SetArgs(args)
			child.SilenceUsage, child.SilenceErrors = true, true
			return child.ExecuteContext(cmd.Context())
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Veil %s\n", version)
	for {
		fmt.Fprint(cmd.OutOrStdout(), operatorMenuText)
		choice, err := operatorMenuRead(reader, cmd.OutOrStdout(), "Choose an action: ")
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if choice == "0" {
			return nil
		}
		err = operatorMenuAction(cmd, reader, choice, execute)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
		}
	}
}

func operatorMenuRead(reader *bufio.Reader, out io.Writer, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	text, err := reader.ReadString('\n')
	if errors.Is(err, io.EOF) && len(text) > 0 {
		err = nil
	}
	return strings.TrimSpace(text), err
}

func operatorMenuConfirm(reader *bufio.Reader, out io.Writer, warning string) (bool, error) {
	answer, err := operatorMenuRead(reader, out, warning+" Type yes to continue [no]: ")
	return answer == "yes", err
}

func operatorMenuAction(cmd *cobra.Command, reader *bufio.Reader, choice string, execute func([]string) error) error {
	out := cmd.OutOrStdout()
	read := func(prompt string) (string, error) { return operatorMenuRead(reader, out, prompt) }
	etcDir, varDir := hostenv.EtcDir(), hostenv.VarDir()
	backups := filepath.Join(varDir, "backups")
	switch choice {
	case "1":
		printOperatorPanelAddress(out, etcDir)
		return operatorSystemCommand(cmd, "systemctl", "--no-pager", "--all", "list-units", "veil*.service", "veil*.socket")
	case "2", "3", "4", "5":
		unit, err := read("Service unit [veil.service] (or veil-helper.service, veil-caddy.service, veil-mieru.service, veil-warp.service, veil-hysteria2@NAME.service, veil-olcrtc@NAME.service): ")
		if err != nil {
			return err
		}
		if unit == "" {
			unit = "veil.service"
		}
		if unit != "veil-helper.service" {
			if err := privileged.DefaultPolicy().ValidateServiceStatus(privileged.ServiceStatusRequest{Units: []string{unit}}); err != nil {
				return err
			}
		}
		if choice == "5" {
			return operatorSystemCommand(cmd, "journalctl", "--no-pager", "-n", "100", "-u", unit)
		}
		action := map[string]string{"2": "start", "3": "stop", "4": "restart"}[choice]
		confirmed, err := operatorMenuConfirm(reader, out, action+" "+unit+"?")
		if err != nil || !confirmed {
			return err
		}
		return operatorSystemCommand(cmd, "systemctl", action, unit)
	case "6":
		return execute([]string{"doctor"})
	case "7", "13", "15":
		args := []string{"update"}
		if choice == "7" {
			args = []string{"repair", "--etc-dir", etcDir, "--var-dir", varDir}
		} else if choice == "15" {
			args = []string{"uninstall", "--keep-data", "--etc-dir", etcDir, "--var-dir", varDir}
		}
		if err := execute(append(append([]string{}, args...), "--dry-run")); err != nil {
			return err
		}
		confirmed, err := operatorMenuConfirm(reader, out, "Apply the displayed "+args[0]+" plan?")
		if err != nil || !confirmed {
			return err
		}
		return execute(append(args, "--yes"))
	case "8":
		confirmed, err := operatorMenuConfirm(reader, out, "Replace all Panel user accounts with a new administrator and revoke existing sessions?")
		if err != nil || !confirmed {
			return err
		}
		return execute([]string{"admin", "reset"})
	case "9", "11":
		var archive string
		if choice == "11" {
			var err error
			archive, err = read("Backup archive path: ")
			if err != nil || archive == "" {
				return err
			}
		}
		passphraseFile, err := read("Passphrase file path (contents will not be displayed): ")
		if err != nil || passphraseFile == "" {
			return err
		}
		if choice == "9" {
			return execute([]string{"backup", "create", "--output-dir", backups, "--passphrase-file", passphraseFile})
		}
		args := []string{"backup", "restore", archive, "--passphrase-file", passphraseFile}
		if err := execute(append(append([]string{}, args...), "--check-only")); err != nil {
			return err
		}
		confirmed, err := operatorMenuConfirm(reader, out, "Overwrite the current management state and encryption key with this backup?")
		if err != nil || !confirmed {
			return err
		}
		return execute(append(args, "--yes"))
	case "10":
		return execute([]string{"backup", "list", "--dir", backups})
	case "12":
		if err := execute([]string{"rollback", "list", "--backup-dir", backups}); err != nil {
			return err
		}
		id, err := read("Configuration backup ID (empty to cancel): ")
		if err != nil || id == "" {
			return err
		}
		confirmed, err := operatorMenuConfirm(reader, out, "Restore configuration files from "+id+"?")
		if err != nil || !confirmed {
			return err
		}
		return execute([]string{"rollback", "restore", id, "--backup-dir", backups, "--yes"})
	case "14":
		names, err := read("Runtimes (comma separated: " + strings.Join(runtimeNames(), ", ") + "; empty to cancel): ")
		if err != nil || names == "" {
			return err
		}
		if err := rejectUnknownRuntimeSelection(strings.Split(names, ",")); err != nil {
			return err
		}
		return execute([]string{"runtime", "install", "--only", names})
	case "16":
		return execute([]string{"install", "--interactive", "--etc-dir", etcDir, "--var-dir", varDir})
	default:
		return fmt.Errorf("unknown choice %q; enter a number from 0 to 16", choice)
	}
}

func printOperatorPanelAddress(out io.Writer, etcDir string) {
	env := hostenv.ReadEnvFile(filepath.Join(etcDir, "veil.env"))
	value := func(key string) string { return hostenv.EnvOrFile(env, key) }
	profile := installer.RURecommendedProfile{
		PanelAccess: value("VEIL_PANEL_ACCESS"), PanelListen: value("VEIL_LISTEN"),
		Domain: value("VEIL_DOMAIN"), WebBasePath: value("VEIL_WEB_BASE_PATH"),
		PanelTLSEnabled: value("VEIL_TLS_CERT") != "",
	}
	if profile.PanelAccess == "local" {
		profile.Domain = ""
	}
	if profile.PanelAccess == "direct" && profile.Domain == "" {
		profile.Domain = strings.Split(value("VEIL_PANEL_PUBLIC_IP"), ",")[0]
		if profile.Domain == "" {
			fmt.Fprintf(out, "Panel listens on %s; the public address is unavailable in veil.env.\n", profile.PanelListen)
			return
		}
	}
	if profile.PanelListen == "" {
		fmt.Fprintln(out, "Panel address is unavailable; check the installed veil.env.")
		return
	}
	fmt.Fprintf(out, "Panel: %s\n", installflow.PanelURL(profile))
}
