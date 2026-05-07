package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const veilGitHubReleasesAPI = "https://api.github.com/repos/mikkelchokolate/Veil/releases/latest"

var versionCheckClient = &http.Client{Timeout: 10 * time.Second}

type VersionCheck struct {
	current string
	out     io.Writer
	latest  func() (string, error)
}

func NewVersionCheck(current string, out io.Writer) VersionCheck {
	return VersionCheck{current: current, out: out, latest: fetchLatestReleaseTag}
}

func (v VersionCheck) Run() error {
	latest, err := v.latest()
	if err != nil {
		return fmt.Errorf("update check failed: %w", err)
	}
	if latest == "" {
		fmt.Fprintln(v.out, "No releases found on GitHub.")
		return nil
	}
	cmp := compareVersions(v.current, latest)
	switch {
	case cmp < 0:
		fmt.Fprintf(v.out, "Newer release available: %s → %s\n", v.current, latest)
		fmt.Fprintf(v.out, "Download: https://github.com/mikkelchokolate/Veil/releases/tag/%s\n", latest)
	case cmp > 0:
		fmt.Fprintf(v.out, "Running a version newer than the latest release (%s > %s).\n", v.current, latest)
	default:
		fmt.Fprintf(v.out, "Veil is up to date (%s).\n", v.current)
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
