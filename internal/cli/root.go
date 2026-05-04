package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	errCommandNotFound = errors.New("command not found")
	commandLookPath    = exec.LookPath
)

func NewRootCommand(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "veil",
		Short: "Veil panel and CLI for NaiveProxy + Hysteria2",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	var checkUpdate bool
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print Veil version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version)
			if checkUpdate {
				return checkLatestVersion(cmd, version)
			}
			return nil
		},
	}
	versionCmd.Flags().BoolVar(&checkUpdate, "check", false, "check for newer Veil releases on GitHub")
	cmd.AddCommand(versionCmd)
	var doctorJSON bool
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run host readiness checks",
		Run: func(cmd *cobra.Command, args []string) {
			printDoctor(cmd, version, doctorJSON)
		},
	}
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "print doctor readiness summary as JSON")
	cmd.AddCommand(doctorCmd)
	cmd.AddCommand(newInstallCommand())
	cmd.AddCommand(newRepairCommand())
	cmd.AddCommand(newRollbackCommand())
	cmd.AddCommand(newServeCommand(version))
	return cmd
}

type doctorSummary struct {
	Version  string                `json:"version"`
	Runtime  string                `json:"runtime"`
	Ready    bool                  `json:"ready"`
	Commands []doctorCommandStatus `json:"commands"`
}

type doctorCommandStatus struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Error    string `json:"error,omitempty"`
	Present  bool   `json:"present"`
	Optional bool   `json:"optional,omitempty"`
}

func printDoctor(cmd *cobra.Command, version string, jsonOutput bool) {
	out := cmd.OutOrStdout()
	summary := buildDoctorSummary(version)
	if jsonOutput {
		_ = json.NewEncoder(out).Encode(summary)
		return
	}
	fmt.Fprintln(out, "Veil doctor")
	fmt.Fprintf(out, "Version: %s\n", summary.Version)
	fmt.Fprintf(out, "Runtime: %s\n", summary.Runtime)
	if summary.Ready {
		fmt.Fprintln(out, "Ready: yes")
	} else {
		fmt.Fprintln(out, "Ready: no")
	}
	fmt.Fprintln(out, "Required commands:")
	for _, command := range summary.Commands {
		if command.Optional {
			continue
		}
		if !command.Present {
			if command.Error != "" {
				fmt.Fprintf(out, "- %s: missing (%s)\n", command.Name, command.Error)
				continue
			}
			fmt.Fprintf(out, "- %s: missing\n", command.Name)
			continue
		}
		fmt.Fprintf(out, "- %s: %s\n", command.Name, command.Path)
	}
	fmt.Fprintln(out, "Optional commands:")
	hasOptional := false
	for _, command := range summary.Commands {
		if !command.Optional {
			continue
		}
		hasOptional = true
		if !command.Present {
			fmt.Fprintf(out, "- %s: missing (warning)\n", command.Name)
			continue
		}
		fmt.Fprintf(out, "- %s: %s\n", command.Name, command.Path)
	}
	if !hasOptional {
		fmt.Fprintln(out, "- none")
	}
}

func buildDoctorSummary(version string) doctorSummary {
	summary := doctorSummary{
		Version: version,
		Runtime: runtime.GOOS + "/" + runtime.GOARCH,
		Ready:   true,
	}
	required := []string{"caddy", "hysteria", "sing-box", "systemctl"}
	optional := []string{"ufw"}

	for _, name := range required {
		status := doctorCommandStatus{Name: name}
		path, err := commandLookPath(name)
		if err == nil {
			status.Path = path
			status.Present = true
		} else {
			status.Error = err.Error()
			summary.Ready = false
		}
		summary.Commands = append(summary.Commands, status)
	}
	for _, name := range optional {
		status := doctorCommandStatus{Name: name, Optional: true}
		path, err := commandLookPath(name)
		if err == nil {
			status.Path = path
			status.Present = true
		} else {
			status.Error = err.Error()
		}
		summary.Commands = append(summary.Commands, status)
	}
	return summary
}

const veilGitHubReleasesAPI = "https://api.github.com/repos/mikkelchokolate/Veil/releases/latest"

var versionCheckClient = &http.Client{Timeout: 10 * time.Second}

// checkLatestVersion fetches the latest Veil release tag from GitHub and
// compares it against the current version. It prints a human-readable
// comparison and returns an error only on network/parse failures.
func checkLatestVersion(cmd *cobra.Command, current string) error {
	out := cmd.OutOrStdout()
	latest, err := fetchLatestReleaseTag()
	if err != nil {
		return fmt.Errorf("update check failed: %w", err)
	}
	if latest == "" {
		fmt.Fprintln(out, "No releases found on GitHub.")
		return nil
	}
	cmp := compareVersions(current, latest)
	switch {
	case cmp < 0:
		fmt.Fprintf(out, "Newer release available: %s → %s\n", current, latest)
		fmt.Fprintf(out, "Download: https://github.com/mikkelchokolate/Veil/releases/tag/%s\n", latest)
	case cmp > 0:
		fmt.Fprintf(out, "Running a version newer than the latest release (%s > %s).\n", current, latest)
	default:
		fmt.Fprintf(out, "Veil is up to date (%s).\n", current)
	}
	return nil
}

// fetchLatestReleaseTag queries the GitHub API for the latest release tag.
func fetchLatestReleaseTag() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, veilGitHubReleasesAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "veil")
	resp, err := versionCheckClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return "", err
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("parse release JSON: %w", err)
	}
	return release.TagName, nil
}

// compareVersions compares two semantic version strings (possibly prefixed with 'v').
// Returns -1 if a < b, 1 if a > b, 0 if equal.
// Non-semver strings are compared lexicographically.
func compareVersions(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}
	for i := 0; i < maxLen; i++ {
		var va, vb int
		if i < len(partsA) {
			fmt.Sscanf(partsA[i], "%d", &va)
		}
		if i < len(partsB) {
			fmt.Sscanf(partsB[i], "%d", &vb)
		}
		if va < vb {
			return -1
		}
		if va > vb {
			return 1
		}
	}
	return 0
}
