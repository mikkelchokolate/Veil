package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const GitHubReleasesAPI = "https://api.github.com/repos/mikkelchokolate/Veil/releases/latest"

var HTTPClient = &http.Client{Timeout: 10 * time.Second}

// releasesAPIURL is overridden by tests so FetchLatestReleaseTag can be
// exercised against a local httptest server instead of the real GitHub API.
var releasesAPIURL = GitHubReleasesAPI

type LatestFunc func() (string, error)

type Check struct {
	current string
	out     io.Writer
	latest  LatestFunc
}

func NewCheck(current string, out io.Writer, latest LatestFunc) Check {
	if latest == nil {
		latest = FetchLatestReleaseTag
	}
	return Check{current: current, out: out, latest: latest}
}

func (v Check) Run() error {
	latest, err := v.latest()
	if err != nil {
		return fmt.Errorf("update check failed: %w", err)
	}
	if latest == "" {
		fmt.Fprintln(v.out, "No releases found on GitHub.")
		return nil
	}
	cmp := Compare(v.current, latest)
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

func FetchLatestReleaseTag() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "veil")
	resp, err := HTTPClient.Do(req)
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

type semver struct {
	major, minor, patch int
	pre                 []preIdent
}

type preIdent struct {
	num   int
	str   string
	isNum bool
}

func Compare(a, b string) int {
	va, okA := parseSemver(a)
	vb, okB := parseSemver(b)
	if !okA && !okB {
		return 0
	}
	if !okA {
		return -1
	}
	if !okB {
		return 1
	}
	return va.compare(vb)
}

// ReleaseTag returns the stamped release tag, stripping a trailing
// " (<commit>)" display suffix used by release binaries.
func ReleaseTag(v string) string {
	v = strings.TrimSpace(v)
	open := strings.LastIndex(v, " (")
	if open < 0 || !strings.HasSuffix(v, ")") {
		return v
	}
	commit := v[open+2 : len(v)-1]
	if !isHexCommit(commit) {
		return v
	}
	tag := strings.TrimSpace(v[:open])
	if tag == "" {
		return v
	}
	return tag
}

func isHexCommit(s string) bool {
	n := len(s)
	if n < 7 || n > 40 {
		return false
	}
	for i := 0; i < n; i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}

func parseSemver(v string) (semver, bool) {
	v = ReleaseTag(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return semver{}, false
	}
	corePre, _, _ := strings.Cut(v, "+")
	core, pre, hasPre := strings.Cut(corePre, "-")
	parts := strings.Split(core, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return semver{}, false
	}
	parsed := semver{}
	for i, part := range parts {
		n, ok := parseNumericIdent(part)
		if !ok {
			return semver{}, false
		}
		switch i {
		case 0:
			parsed.major = n
		case 1:
			parsed.minor = n
		case 2:
			parsed.patch = n
		}
	}
	if !hasPre {
		return parsed, true
	}
	if pre == "" {
		return semver{}, false
	}
	for _, ident := range strings.Split(pre, ".") {
		if ident == "" {
			return semver{}, false
		}
		if isAllDigits(ident) {
			n, ok := parseNumericIdent(ident)
			if !ok {
				return semver{}, false
			}
			parsed.pre = append(parsed.pre, preIdent{num: n, isNum: true})
			continue
		}
		if !isPrereleaseIdent(ident) {
			return semver{}, false
		}
		parsed.pre = append(parsed.pre, preIdent{str: ident})
	}
	return parsed, true
}

func (a semver) compare(b semver) int {
	if c := compareInt(a.major, b.major); c != 0 {
		return c
	}
	if c := compareInt(a.minor, b.minor); c != 0 {
		return c
	}
	if c := compareInt(a.patch, b.patch); c != 0 {
		return c
	}
	if len(a.pre) == 0 && len(b.pre) == 0 {
		return 0
	}
	if len(a.pre) == 0 {
		return 1
	}
	if len(b.pre) == 0 {
		return -1
	}
	n := min(len(a.pre), len(b.pre))
	for i := 0; i < n; i++ {
		if c := a.pre[i].compare(b.pre[i]); c != 0 {
			return c
		}
	}
	return compareInt(len(a.pre), len(b.pre))
}

func (a preIdent) compare(b preIdent) int {
	if a.isNum && b.isNum {
		return compareInt(a.num, b.num)
	}
	if a.isNum {
		return -1
	}
	if b.isNum {
		return 1
	}
	if a.str < b.str {
		return -1
	}
	if a.str > b.str {
		return 1
	}
	return 0
}

func parseNumericIdent(s string) (int, bool) {
	if s == "" || !isAllDigits(s) {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		n = n*10 + int(s[i]-'0')
		if n < 0 {
			return 0, false
		}
	}
	return n, true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isPrereleaseIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-' {
			continue
		}
		return false
	}
	return true
}

func compareInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
