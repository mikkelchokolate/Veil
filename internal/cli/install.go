package cli

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
)

func newInstallCommand() *cobra.Command {
	var profile string
	var domain string
	var email string
	var dryRun bool
	var yes bool
	var etcDir string
	var varDir string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install and configure Veil managed services",
		RunE: func(cmd *cobra.Command, args []string) error {
			if profile != "ru-recommended" {
				return fmt.Errorf("profile %q is not implemented yet", profile)
			}
			if domain == "" {
				return fmt.Errorf("--domain is required for ru-recommended profile")
			}
			if email == "" {
				return fmt.Errorf("--email is required for ru-recommended profile")
			}

			availability, err := installer.DetectPortAvailability([]int{443, 8443})
			if err != nil {
				return err
			}
			randomPort := func() int {
				port, err := installer.RandomHighPort()
				if err != nil {
					return 31874
				}
				return port
			}
			built, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
				Domain:       domain,
				Email:        email,
				Availability: availability,
				Secret:       randomSecret,
				RandomPort:   randomPort,
			})
			if err != nil {
				return err
			}
			printRURecommended(cmd, built, dryRun)
			if dryRun {
				return nil
			}
			if !yes {
				return fmt.Errorf("apply mode requires --yes; rerun with --dry-run to preview")
			}
			result, err := installer.ApplyRURecommendedProfile(built, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Written files:")
			for _, path := range result.WrittenFiles {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", path)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "default", "install profile: default or ru-recommended")
	cmd.Flags().StringVar(&domain, "domain", "", "domain for ACME and client configs")
	cmd.Flags().StringVar(&email, "email", "", "ACME email")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render installation plan without writing files")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm writing generated files")
	cmd.Flags().StringVar(&etcDir, "etc-dir", "/etc/veil", "Veil configuration directory")
	cmd.Flags().StringVar(&varDir, "var-dir", "/var/lib/veil", "Veil state directory")
	return cmd
}

func printRURecommended(cmd *cobra.Command, profile installer.RURecommendedProfile, dryRun bool) {
	mode := "apply"
	if dryRun {
		mode = "dry run"
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Veil ru-recommended %s\n", mode)
	fmt.Fprintf(out, "Domain: %s\n", profile.Domain)
	fmt.Fprintf(out, "Email: %s\n", profile.Email)
	if profile.PortPlan.Changed {
		fmt.Fprintf(out, "Port changed: %s\n", profile.PortPlan.Reason)
	}
	fmt.Fprintf(out, "NaiveProxy TCP port: %d\n", profile.PortPlan.Naive.Port)
	fmt.Fprintf(out, "Hysteria2 UDP port: %d\n", profile.PortPlan.Hysteria2.Port)
	fmt.Fprintf(out, "NaiveProxy client URL: %s\n", profile.NaiveClientURL)
	fmt.Fprintf(out, "Hysteria2 client URI: %s\n", profile.Hysteria2ClientURI)
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Generated Caddyfile")
	fmt.Fprintln(out, strings.Repeat("-", 24))
	fmt.Fprintln(out, profile.Caddyfile)
	fmt.Fprintln(out, "Generated Hysteria2 server.yaml")
	fmt.Fprintln(out, strings.Repeat("-", 32))
	fmt.Fprintln(out, profile.Hysteria2YAML)
}

func randomSecret(label string) string {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return label + "-change-me"
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
