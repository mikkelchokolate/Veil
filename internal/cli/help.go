package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

const operatorHelpIntro = "Install the Panel first. Add NaiveProxy, Hysteria2, olcRTC, and Mieru as Inbounds from the Panel."

func attachOperatorHelp(root *cobra.Command) {
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd.Parent() == nil {
			printOperatorCatalog(cmd, cmd.OutOrStdout())
			return
		}
		defaultHelp(cmd, args)
	})
}

func printOperatorCatalog(root *cobra.Command, w io.Writer) {
	fmt.Fprintln(w, strings.TrimSpace(root.Short))
	fmt.Fprintln(w)
	fmt.Fprintln(w, operatorHelpIntro)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  veil [command]")
	fmt.Fprintln(w, "  veil help [command]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	for _, line := range operatorCatalogLines(root) {
		fmt.Fprintln(tw, line)
	}
	_ = tw.Flush()
	fmt.Fprintln(w)
	fmt.Fprintln(w, `Use "veil help [command]" for more information about a command.`)
}

func operatorCatalogLines(root *cobra.Command) []string {
	var lines []string
	collectOperatorCatalog(root, "veil", &lines)
	return lines
}

func collectOperatorCatalog(cmd *cobra.Command, path string, lines *[]string) {
	for _, child := range cmd.Commands() {
		if child.Hidden || !child.IsAvailableCommand() {
			continue
		}
		childPath := path + " " + child.Name()
		*lines = append(*lines, fmt.Sprintf("  %s\t%s", childPath, child.Short))
		collectOperatorCatalog(child, childPath, lines)
	}
}
