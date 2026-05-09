package cli

import (
	"io"
	"net/http"
	"time"

	versionflow "github.com/veil-panel/veil/internal/cliflow/version"
)

const veilGitHubReleasesAPI = versionflow.GitHubReleasesAPI

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
	return versionflow.NewCheck(v.current, v.out, v.latest).Run()
}

func fetchLatestReleaseTag() (string, error) {
	old := versionflow.HTTPClient
	versionflow.HTTPClient = versionCheckClient
	defer func() { versionflow.HTTPClient = old }()
	return versionflow.FetchLatestReleaseTag()
}

func compareVersions(a, b string) int {
	return versionflow.Compare(a, b)
}
